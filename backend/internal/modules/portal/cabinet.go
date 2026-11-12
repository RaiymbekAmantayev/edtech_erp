package portal

import (
	"context"
	"net/http"
	"time"

	"emp/internal/modules/exam"
	"emp/internal/platform/httpx"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

// me — профиль ученика или родителя (у родителя — список детей).
func (h *Handler) me(w http.ResponseWriter, r *http.Request) error {
	a := accountFrom(r.Context())
	ctx := r.Context()
	out := map[string]any{"kind": a.Kind}
	err := h.DB.InTenant(ctx, a.TenantID, func(tx pgx.Tx) error {
		var school string
		var tg *int64
		tx.QueryRow(ctx, `SELECT name FROM tenants WHERE id = app_current_tenant()`).Scan(&school)
		tx.QueryRow(ctx, `SELECT telegram_id FROM portal_accounts WHERE id=$1`, a.ID).Scan(&tg)
		out["school"] = school
		out["telegram_linked"] = tg != nil
		if a.Kind == "student" {
			s, err := studentProfile(ctx, tx, *a.StudentID)
			if err != nil {
				return err
			}
			out["profile"] = s
			out["children"] = []map[string]any{{"id": s["id"], "full_name": s["full_name"]}}
			return nil
		}
		var name string
		var phone, email *string
		var chat *int64
		if err := tx.QueryRow(ctx, `SELECT full_name, phone, email, telegram_chat_id FROM parents WHERE id=$1`, *a.ParentID).Scan(&name, &phone, &email, &chat); err != nil {
			return err
		}
		out["profile"] = map[string]any{"id": *a.ParentID, "full_name": name, "phone": phone, "email": email, "notifications": chat != nil}
		rows, err := tx.Query(ctx, `SELECT s.id::text, s.full_name, sp.relation FROM student_parents sp JOIN students s ON s.id=sp.student_id
			WHERE sp.parent_id=$1 AND s.archived_at IS NULL ORDER BY s.full_name`, *a.ParentID)
		if err != nil {
			return err
		}
		children := []map[string]any{}
		for rows.Next() {
			var id, n, rel string
			rows.Scan(&id, &n, &rel)
			children = append(children, map[string]any{"id": id, "full_name": n, "relation": rel})
		}
		rows.Close()
		out["children"] = children
		return nil
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, out)
}

func studentProfile(ctx context.Context, tx pgx.Tx, id string) (map[string]any, error) {
	var name, status, branch string
	var number int64
	var birth *time.Time
	var phone, email *string
	if err := tx.QueryRow(ctx, `SELECT s.full_name, s.number, s.status, s.birth_date, s.phone, s.email, b.name
		FROM students s JOIN branches b ON b.id=s.branch_id WHERE s.id=$1`, id).Scan(&name, &number, &status, &birth, &phone, &email, &branch); err != nil {
		return nil, err
	}
	return map[string]any{"id": id, "full_name": name, "number": number, "status": status, "birth_date": birth, "phone": phone, "email": email, "branch": branch}, nil
}

// canSee — ученик видит только себя, родитель — своих детей.
func canSee(ctx context.Context, tx pgx.Tx, a *Account, studentID string) bool {
	if a.Kind == "student" {
		return a.StudentID != nil && *a.StudentID == studentID
	}
	var ok bool
	tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM student_parents WHERE parent_id=$1 AND student_id=$2)`, *a.ParentID, studentID).Scan(&ok)
	return ok
}

// studentOverview — группы, ближайшие занятия, посещаемость, абонементы и баланс ученика.
func (h *Handler) studentOverview(w http.ResponseWriter, r *http.Request) error {
	a := accountFrom(r.Context())
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	if !validUUID(id) {
		return httpx.NotFound("STUDENT_NOT_FOUND")
	}
	out := map[string]any{}
	err := h.DB.InTenant(ctx, a.TenantID, func(tx pgx.Tx) error {
		if !canSee(ctx, tx, a, id) {
			return httpx.NotFound("STUDENT_NOT_FOUND")
		}
		profile, err := studentProfile(ctx, tx, id)
		if err != nil {
			return err
		}
		out["profile"] = profile
		var balance int64
		tx.QueryRow(ctx, `SELECT coalesce((SELECT balance_tiyn FROM student_balances WHERE student_id=$1), 0)`, id).Scan(&balance)
		out["balance_tiyn"] = balance

		groups, err := collect(ctx, tx, `SELECT g.id::text, g.name, c.name, e.full_name
			FROM group_students gs JOIN groups g ON g.id=gs.group_id JOIN courses c ON c.id=g.course_id LEFT JOIN employees e ON e.id=g.teacher_id
			WHERE gs.student_id=$1 AND gs.left_at IS NULL AND g.status='active' ORDER BY g.name`,
			[]any{id}, func(rows pgx.Rows) (map[string]any, error) {
				var gid, name, course string
				var teacher *string
				err := rows.Scan(&gid, &name, &course, &teacher)
				return map[string]any{"id": gid, "name": name, "course": course, "teacher": teacher}, err
			})
		if err != nil {
			return err
		}
		out["groups"] = groups

		out["upcoming"], err = collect(ctx, tx, `SELECT l.id::text, l.starts_at, l.ends_at, l.status, g.name, e.full_name, cl.name, l.topic, l.meeting_url
			FROM lessons l JOIN group_students gs ON gs.group_id=l.group_id AND gs.student_id=$1 AND gs.left_at IS NULL
			JOIN groups g ON g.id=l.group_id LEFT JOIN employees e ON e.id=l.teacher_id LEFT JOIN classrooms cl ON cl.id=l.classroom_id
			WHERE l.ends_at >= NOW() AND l.starts_at < NOW() + interval '14 days' ORDER BY l.starts_at LIMIT 40`,
			[]any{id}, scanLesson)
		if err != nil {
			return err
		}

		out["attendance"], err = collect(ctx, tx, `SELECT l.starts_at, g.name, a.status, l.topic
			FROM attendance a JOIN lessons l ON l.id=a.lesson_id JOIN groups g ON g.id=l.group_id
			WHERE a.student_id=$1 ORDER BY l.starts_at DESC LIMIT 30`,
			[]any{id}, func(rows pgx.Rows) (map[string]any, error) {
				var at time.Time
				var group, status string
				var topic *string
				err := rows.Scan(&at, &group, &status, &topic)
				return map[string]any{"starts_at": at, "group": group, "status": status, "topic": topic}, err
			})
		if err != nil {
			return err
		}
		stats := map[string]int{}
		rows, err := tx.Query(ctx, `SELECT a.status, count(*) FROM attendance a JOIN lessons l ON l.id=a.lesson_id
			WHERE a.student_id=$1 AND l.starts_at > NOW() - interval '90 days' GROUP BY 1`, id)
		if err != nil {
			return err
		}
		for rows.Next() {
			var s string
			var n int
			rows.Scan(&s, &n)
			stats[s] = n
		}
		rows.Close()
		out["attendance_90d"] = stats

		out["subscriptions"], err = collect(ctx, tx, `SELECT s.name, s.type, s.lessons_total, coalesce((SELECT sum(qty) FROM subscription_usages u WHERE u.subscription_id=s.id),0),
				s.starts_on, s.ends_on, s.status
			FROM subscriptions s WHERE s.student_id=$1 AND s.status IN ('active','frozen') ORDER BY s.starts_on DESC`,
			[]any{id}, func(rows pgx.Rows) (map[string]any, error) {
				var name, typ, status string
				var total *int
				var used int
				var starts time.Time
				var ends *time.Time
				err := rows.Scan(&name, &typ, &total, &used, &starts, &ends, &status)
				m := map[string]any{"name": name, "type": typ, "lessons_total": total, "lessons_used": used, "starts_on": starts, "ends_on": ends, "status": status}
				if total != nil {
					m["lessons_left"] = *total - used
				}
				return m, err
			})
		if err != nil {
			return err
		}
		out["tests"], err = exam.LoadResults(ctx, tx, "st.student_id = $1", []any{id}, 30)
		return err
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, out)
}

func scanLesson(rows pgx.Rows) (map[string]any, error) {
	var id, status, group string
	var starts, ends time.Time
	var teacher, room, topic, meeting *string
	err := rows.Scan(&id, &starts, &ends, &status, &group, &teacher, &room, &topic, &meeting)
	return map[string]any{"id": id, "starts_at": starts, "ends_at": ends, "status": status, "group": group, "teacher": teacher,
		"classroom": room, "topic": topic, "meeting_url": meeting}, err
}

func collect(ctx context.Context, tx pgx.Tx, q string, args []any, scan func(pgx.Rows) (map[string]any, error)) ([]map[string]any, error) {
	rows, err := tx.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		m, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
