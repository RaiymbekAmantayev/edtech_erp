package schedule

import (
	"context"
	"net/http"
	"slices"
	"time"

	"emp/internal/modules/payroll"
	"emp/internal/platform/auth"
	"emp/internal/platform/database"
	"emp/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

type substituteOption struct {
	ID       string   `json:"id"`
	FullName string   `json:"full_name"`
	Subjects []string `json:"subjects"`
	Free     bool     `json:"free"`
	Busy     *string  `json:"busy"` // чем занят в это время
	Current  bool     `json:"current"`
	Main     bool     `json:"main"` // основной преподаватель группы
}

// substitutes — преподаватели филиала занятия: свободные в это время — первыми, занятые — с причиной.
func (h *Handler) substitutes(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	p := auth.FromContext(r.Context())
	ctx := r.Context()
	out := []substituteOption{}
	err := h.DB.Tx(ctx, func(tx pgx.Tx) error {
		var branchID string
		var teacherID, mainID *string
		var starts, ends time.Time
		if err := tx.QueryRow(ctx, `SELECT l.branch_id::text, l.teacher_id::text, g.teacher_id::text, l.starts_at, l.ends_at FROM lessons l JOIN groups g ON g.id=l.group_id WHERE l.id=$1`, id).
			Scan(&branchID, &teacherID, &mainID, &starts, &ends); err != nil {
			return err
		}
		if !p.CanBranch(branchID) {
			return httpx.NotFound("LESSON_NOT_FOUND")
		}
		rows, err := tx.Query(ctx, `SELECT e.id::text, e.full_name, e.subjects,
				(SELECT g.name || ' ' || to_char(x.starts_at AT TIME ZONE b.timezone, 'HH24:MI') || '–' || to_char(x.ends_at AT TIME ZONE b.timezone, 'HH24:MI')
				 FROM lessons x JOIN groups g ON g.id=x.group_id JOIN branches b ON b.id=x.branch_id
				 WHERE x.teacher_id=e.id AND x.id<>$1 AND x.status<>'cancelled' AND x.starts_at < $4 AND x.ends_at > $3 ORDER BY x.starts_at LIMIT 1)
			FROM employees e WHERE e.status='active' AND e.is_teacher AND (cardinality(e.branch_ids)=0 OR $2::uuid = ANY(e.branch_ids))
			ORDER BY e.full_name`, id, branchID, starts, ends)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var o substituteOption
			if err := rows.Scan(&o.ID, &o.FullName, &o.Subjects, &o.Busy); err != nil {
				return err
			}
			o.Free = o.Busy == nil
			o.Current = teacherID != nil && *teacherID == o.ID
			o.Main = mainID != nil && *mainID == o.ID
			out = append(out, o)
		}
		return rows.Err()
	})
	if err != nil {
		return err
	}
	slices.SortStableFunc(out, func(a, b substituteOption) int {
		if a.Free != b.Free {
			if a.Free {
				return -1
			}
			return 1
		}
		return 0
	})
	return httpx.OK(w, out)
}

// substitute ставит на занятие другого преподавателя, только если он свободен в это время.
// Начисление за урок получает фактический преподаватель; основной преподаватель группы запоминается в substitute_for.
func (h *Handler) substitute(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		TeacherID string `json:"teacher_id"`
		Reason    string `json:"reason"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.TeacherID == "" {
		return (&httpx.Validation{Fields: []httpx.FieldError{{Field: "teacher_id", Code: "REQUIRED"}}}).Err()
	}
	id := chi.URLParam(r, "id")
	p := auth.FromContext(r.Context())
	ctx := r.Context()
	err := h.DB.Tx(ctx, func(tx pgx.Tx) error {
		var branchID, status string
		var oldTeacher *string
		var starts, ends time.Time
		if err := tx.QueryRow(ctx, `SELECT branch_id::text, status, teacher_id::text, starts_at, ends_at FROM lessons WHERE id=$1 FOR UPDATE`, id).
			Scan(&branchID, &status, &oldTeacher, &starts, &ends); err != nil {
			return err
		}
		if !p.CanBranch(branchID) {
			return httpx.NotFound("LESSON_NOT_FOUND")
		}
		if status == "cancelled" {
			return httpx.Conflict("LESSON_CANCELLED", "Занятие отменено")
		}
		if oldTeacher != nil && *oldTeacher == req.TeacherID {
			return nil
		}
		var ok bool
		tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM employees WHERE id=$1 AND status='active' AND is_teacher AND (cardinality(branch_ids)=0 OR $2::uuid = ANY(branch_ids)))`,
			req.TeacherID, branchID).Scan(&ok)
		if !ok {
			return (&httpx.Validation{Fields: []httpx.FieldError{{Field: "teacher_id", Code: "INVALID_OPTION"}}}).Err()
		}
		var busy *string
		tx.QueryRow(ctx, `SELECT g.name FROM lessons x JOIN groups g ON g.id=x.group_id
			WHERE x.teacher_id=$1 AND x.id<>$2 AND x.status<>'cancelled' AND x.starts_at < $4 AND x.ends_at > $3 LIMIT 1`, req.TeacherID, id, starts, ends).Scan(&busy)
		if busy != nil {
			return httpx.Conflict("TEACHER_BUSY", "Преподаватель в это время ведёт «"+*busy+"»")
		}
		if err := SetTeacher(ctx, tx, id, &req.TeacherID); err != nil {
			return err
		}
		database.Publish(ctx, tx, "lesson.substituted", map[string]any{"lesson_id": id, "teacher_id": req.TeacherID})
		return database.Audit(ctx, tx, "lesson", id, "teacher_substituted", map[string]any{"teacher_id": oldTeacher}, map[string]any{"teacher_id": req.TeacherID, "reason": req.Reason})
	})
	if err != nil {
		return err
	}
	return httpx.NoContent(w)
}

// SetTeacher меняет преподавателя занятия: замена отмечается, если урок ведёт не основной преподаватель группы;
// у проведённого урока начисление переходит новому преподавателю.
func SetTeacher(ctx context.Context, tx pgx.Tx, lessonID string, teacherID *string) error {
	if _, err := tx.Exec(ctx, `UPDATE lessons l SET teacher_id=$2::uuid,
			substitute_for = CASE WHEN g.teacher_id IS NULL OR g.teacher_id = $2::uuid OR $2::uuid IS NULL THEN NULL ELSE g.teacher_id END
		FROM groups g WHERE g.id=l.group_id AND l.id=$1`, lessonID, teacherID); err != nil {
		return err
	}
	return payroll.Recalc(ctx, tx, lessonID, false)
}
