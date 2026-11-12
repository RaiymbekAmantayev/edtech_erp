// Пакет attendance — отметка посещаемости, закрытие занятия и списания по правилам (ТЗ разделы 12, 15.6).
package attendance

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"emp/internal/modules/customfield"
	"emp/internal/modules/exam"
	"emp/internal/modules/finance"
	"emp/internal/modules/ktp"
	"emp/internal/modules/payroll"
	"emp/internal/platform/auth"
	"emp/internal/platform/database"
	"emp/internal/platform/httpx"
	mw "emp/internal/platform/middleware"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

type Handler struct{ DB *database.DB }

var Statuses = []string{"present", "late", "absent", "excused", "trial"}

func (h *Handler) Routes(r chi.Router) {
	r.With(mw.Perm("attendance.view", "attendance.mark")).Get("/lessons/{id}/attendance", httpx.Handler(h.get).ServeHTTP)
	r.With(mw.Perm("attendance.mark")).Put("/lessons/{id}/attendance", httpx.Handler(h.save).ServeHTTP)
	r.With(mw.Perm("attendance.view")).Get("/attendance/history", httpx.Handler(h.history).ServeHTTP)
	r.With(mw.Perm("attendance.view")).Get("/attendance/report", httpx.Handler(h.report).ServeHTTP)
}

type rosterItem struct {
	StudentID *string        `json:"student_id"`
	LeadID    *string        `json:"lead_id"`
	FullName  string         `json:"full_name"`
	IsTrial   bool           `json:"is_trial"`
	Status    *string        `json:"status"`
	Reason    *string        `json:"reason"`
	Comment   *string        `json:"comment"`
	Info      map[string]any `json:"info"`
	Note      string         `json:"note,omitempty"`
}

// canAccess: администратор — по правам и филиалу; преподаватель — только свои занятия.
func canAccess(p *auth.Principal, teacherID *string, branchID string) bool {
	if p.Can("attendance.view") || p.Can("schedule.view") {
		return p.CanBranch(branchID)
	}
	return teacherID != nil && *teacherID == p.EmployeeID && p.EmployeeID != ""
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	p := auth.FromContext(r.Context())
	ctx := r.Context()
	out := map[string]any{}
	err := h.DB.Tx(ctx, func(tx pgx.Tx) error {
		var groupID, branchID, status, groupName, tz, courseID string
		var teacherID, room, topic, homework, ktpTopicID *string
		var starts, ends time.Time
		var closedAt *time.Time
		if err := tx.QueryRow(ctx, `SELECT l.group_id, l.branch_id, l.teacher_id::text, l.status, g.name, b.timezone, cl.name, l.topic, l.starts_at, l.ends_at, l.closed_at,
				g.course_id::text, l.homework, l.ktp_topic_id::text
			FROM lessons l JOIN groups g ON g.id=l.group_id JOIN branches b ON b.id=l.branch_id LEFT JOIN classrooms cl ON cl.id=l.classroom_id WHERE l.id=$1`, id).
			Scan(&groupID, &branchID, &teacherID, &status, &groupName, &tz, &room, &topic, &starts, &ends, &closedAt, &courseID, &homework, &ktpTopicID); err != nil {
			return err
		}
		if !canAccess(p, teacherID, branchID) {
			return httpx.NotFound("LESSON_NOT_FOUND")
		}
		roster, err := Roster(ctx, tx, id)
		if err != nil {
			return err
		}
		// в кабинете преподавателя показываются только поля с флагом «В кабинете преподавателя»
		fields, _ := customfield.Load(ctx, tx, "student", false)
		teacherFields := []customfield.Field{}
		for _, f := range fields {
			if f.Settings.ShowInTeacher && customfield.CanView(&f, p) {
				teacherFields = append(teacherFields, f)
			}
		}
		for i := range roster {
			if roster[i].StudentID == nil {
				continue
			}
			var cf map[string]any
			tx.QueryRow(ctx, `SELECT custom_fields FROM students WHERE id=$1`, *roster[i].StudentID).Scan(&cf)
			info := map[string]any{}
			for j := range teacherFields {
				f := &teacherFields[j]
				if v, ok := cf[f.Key]; ok && v != nil {
					info[f.Key] = map[string]string{"label": f.LabelFor("ru"), "value": customfield.Display(f, v, "ru")}
				}
			}
			roster[i].Info = info
			// действующая справка/заморозка — подсказка преподавателю
			var frozenTo *time.Time
			tx.QueryRow(ctx, `SELECT max(f.ends_on) FROM subscription_freezes f JOIN subscriptions s ON s.id=f.subscription_id
				WHERE s.student_id=$1 AND $2::date BETWEEN f.starts_on AND f.ends_on`, *roster[i].StudentID, starts).Scan(&frozenTo)
			if frozenTo != nil {
				roster[i].Note = "заморозка до " + frozenTo.Format("02.01")
			}
		}
		var editHours int
		tx.QueryRow(ctx, `SELECT coalesce((settings->>'edit_closed_hours')::int, 24) FROM tenants WHERE id = app_current_tenant()`).Scan(&editHours)
		editable := status != "done" || p.Can("attendance.edit_closed") || (closedAt != nil && time.Since(*closedAt) < time.Duration(editHours)*time.Hour)
		out = map[string]any{"lesson": map[string]any{"id": id, "group_id": groupID, "group_name": groupName, "status": status, "classroom": room, "topic": topic,
			"homework": homework, "ktp_topic_id": ktpTopicID, "starts_at": starts, "ends_at": ends, "closed_at": closedAt, "timezone": tz},
			"roster": roster, "editable": editable, "requires_reason": status == "done" && closedAt != nil && time.Since(*closedAt) >= time.Duration(editHours)*time.Hour}
		// КТП: темы с ДЗ для выбора и следующая непройденная тема
		cats, suggested, err := ktp.ForLesson(ctx, tx, groupID, courseID)
		if err != nil {
			return err
		}
		out["ktp"] = map[string]any{"categories": cats, "suggested_topic_id": suggested}
		// тесты за урок (обязательные и необязательные) и уже введённые баллы
		tests, grades, results, err := exam.ForLesson(ctx, tx, id, courseID)
		if err != nil {
			return err
		}
		out["tests"], out["grades"], out["results"] = tests, grades, results
		return nil
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, out)
}

// Roster — состав занятия на дату: члены группы на дату занятия + пробные/разовые гости.
func Roster(ctx context.Context, tx pgx.Tx, lessonID string) ([]rosterItem, error) {
	rows, err := tx.Query(ctx, `
		SELECT s.id::text, NULL::text, s.full_name, false, a.status, a.reason, a.comment
		FROM lessons l JOIN branches b ON b.id=l.branch_id
		JOIN group_students gs ON gs.group_id=l.group_id AND gs.joined_at <= (l.starts_at AT TIME ZONE b.timezone)::date
			AND (gs.left_at IS NULL OR gs.left_at > (l.starts_at AT TIME ZONE b.timezone)::date)
		JOIN students s ON s.id=gs.student_id
		LEFT JOIN attendance a ON a.lesson_id=l.id AND a.student_id=s.id
		WHERE l.id=$1
		UNION ALL
		SELECT lg.student_id::text, lg.lead_id::text, coalesce(s.full_name, ld.full_name), lg.is_trial, a.status, a.reason, a.comment
		FROM lesson_guests lg LEFT JOIN students s ON s.id=lg.student_id LEFT JOIN leads ld ON ld.id=lg.lead_id
		LEFT JOIN attendance a ON a.lesson_id=lg.lesson_id AND (a.student_id=lg.student_id OR a.lead_id=lg.lead_id)
		WHERE lg.lesson_id=$1
		UNION ALL
		-- отметки учеников, которые уже выбыли из группы (история сохраняется)
		SELECT s.id::text, NULL, s.full_name, false, a.status, a.reason, a.comment FROM attendance a JOIN students s ON s.id=a.student_id
		WHERE a.lesson_id=$1 AND NOT EXISTS (SELECT 1 FROM lessons l JOIN group_students gs ON gs.group_id=l.group_id WHERE l.id=$1 AND gs.student_id=a.student_id)
			AND NOT EXISTS (SELECT 1 FROM lesson_guests lg WHERE lg.lesson_id=$1 AND lg.student_id=a.student_id)
		ORDER BY 4, 3`, lessonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []rosterItem{}
	seen := map[string]bool{}
	for rows.Next() {
		var it rosterItem
		if err := rows.Scan(&it.StudentID, &it.LeadID, &it.FullName, &it.IsTrial, &it.Status, &it.Reason, &it.Comment); err != nil {
			return nil, err
		}
		key := deref(it.StudentID) + "|" + deref(it.LeadID)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, it)
	}
	return out, rows.Err()
}

type saveReq struct {
	Items []struct {
		StudentID string `json:"student_id"`
		LeadID    string `json:"lead_id"`
		Status    string `json:"status"`
		Reason    string `json:"reason"`
		Comment   string `json:"comment"`
	} `json:"items"`
	Topic        *string            `json:"topic"`
	Homework     *string            `json:"homework"`
	KtpTopicID   *string            `json:"ktp_topic_id"`
	Tests        []exam.LessonEntry `json:"tests"`
	Close        bool               `json:"close"`
	ChangeReason string             `json:"change_reason"`
}

func (h *Handler) save(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	var req saveReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	p := auth.FromContext(r.Context())
	ctx := r.Context()
	result := map[string]any{}
	err := h.DB.Tx(ctx, func(tx pgx.Tx) error {
		var branchID, status, groupID, courseID, tz string
		var teacherID *string
		var closedAt *time.Time
		var starts time.Time
		if err := tx.QueryRow(ctx, `SELECT l.branch_id, l.teacher_id::text, l.status, l.closed_at, l.starts_at, l.group_id::text, g.course_id::text, b.timezone
			FROM lessons l JOIN groups g ON g.id=l.group_id JOIN branches b ON b.id=l.branch_id WHERE l.id=$1 FOR UPDATE OF l`, id).
			Scan(&branchID, &teacherID, &status, &closedAt, &starts, &groupID, &courseID, &tz); err != nil {
			return err
		}
		if !canAccess(p, teacherID, branchID) {
			return httpx.NotFound("LESSON_NOT_FOUND")
		}
		if status == "cancelled" {
			return httpx.Conflict("LESSON_CANCELLED", "Занятие отменено")
		}
		if starts.After(time.Now().Add(30*time.Minute)) && (len(req.Items) > 0 || len(req.Tests) > 0 || req.Close) {
			return httpx.Conflict("LESSON_NOT_STARTED", "Отметить посещаемость можно после начала занятия")
		}
		// редактирование закрытого занятия: в течение N часов — преподаватель, позже — только с правом и причиной (ТЗ 12.5)
		if status == "done" && closedAt != nil {
			var editHours int
			tx.QueryRow(ctx, `SELECT coalesce((settings->>'edit_closed_hours')::int, 24) FROM tenants WHERE id = app_current_tenant()`).Scan(&editHours)
			if time.Since(*closedAt) >= time.Duration(editHours)*time.Hour {
				if !p.Can("attendance.edit_closed") {
					return httpx.NewError(403, "ATTENDANCE_LOCKED", "Срок редактирования посещаемости истёк")
				}
				if strings.TrimSpace(req.ChangeReason) == "" {
					return (&httpx.Validation{Fields: []httpx.FieldError{{Field: "change_reason", Code: "REQUIRED"}}}).Err()
				}
			}
		}
		roster, err := Roster(ctx, tx, id)
		if err != nil {
			return err
		}
		allowed := map[string]bool{}
		for _, it := range roster {
			allowed[deref(it.StudentID)+"|"+deref(it.LeadID)] = true
		}
		changed := 0
		for _, it := range req.Items {
			if !slices.Contains(Statuses, it.Status) {
				return (&httpx.Validation{Fields: []httpx.FieldError{{Field: "items.status", Code: "INVALID"}}}).Err()
			}
			if !allowed[it.StudentID+"|"+it.LeadID] {
				return httpx.BadRequest("NOT_IN_ROSTER", "Ученик не числится на занятии")
			}
			var attID string
			var old *string
			if it.StudentID != "" {
				tx.QueryRow(ctx, `SELECT id, status FROM attendance WHERE lesson_id=$1 AND student_id=$2`, id, it.StudentID).Scan(&attID, &old)
			} else {
				tx.QueryRow(ctx, `SELECT id, status FROM attendance WHERE lesson_id=$1 AND lead_id=$2`, id, it.LeadID).Scan(&attID, &old)
			}
			if old != nil && *old == it.Status {
				tx.Exec(ctx, `UPDATE attendance SET reason=NULLIF($2,''), comment=NULLIF($3,'') WHERE id=$1`, attID, it.Reason, it.Comment)
				continue
			}
			if attID == "" {
				if err := tx.QueryRow(ctx, `INSERT INTO attendance (tenant_id, lesson_id, student_id, lead_id, status, reason, comment, marked_by)
					VALUES (app_current_tenant(), $1, NULLIF($2,'')::uuid, NULLIF($3,'')::uuid, $4, NULLIF($5,''), NULLIF($6,''), $7) RETURNING id`,
					id, it.StudentID, it.LeadID, it.Status, it.Reason, it.Comment, p.UserID).Scan(&attID); err != nil {
					return err
				}
			} else if _, err := tx.Exec(ctx, `UPDATE attendance SET status=$2, reason=NULLIF($3,''), comment=NULLIF($4,''), marked_by=$5, marked_at=NOW() WHERE id=$1`,
				attID, it.Status, it.Reason, it.Comment, p.UserID); err != nil {
				return err
			}
			// бесследно менять посещаемость нельзя: каждое изменение — в историю (старое, новое, кто, когда, причина)
			if _, err := tx.Exec(ctx, `INSERT INTO attendance_history (tenant_id, attendance_id, lesson_id, student_id, old_status, new_status, reason, changed_by)
				VALUES (app_current_tenant(), $1, $2, NULLIF($3,'')::uuid, $4, $5, NULLIF($6,''), $7)`,
				attID, id, it.StudentID, old, it.Status, firstNonEmpty(req.ChangeReason, it.Reason), p.UserID); err != nil {
				return err
			}
			if old != nil && it.StudentID != "" {
				database.Audit(ctx, tx, "student", it.StudentID, "attendance_changed", map[string]any{"status": *old, "lesson_id": id, "starts_at": starts},
					map[string]any{"status": it.Status, "reason": firstNonEmpty(req.ChangeReason, it.Reason)})
			}
			if it.StudentID != "" && (it.Status == "absent" || it.Status == "late") {
				database.Publish(ctx, tx, "attendance.marked", map[string]any{"lesson_id": id, "student_id": it.StudentID, "status": it.Status})
			}
			if it.LeadID != "" {
				tx.Exec(ctx, `UPDATE leads SET stage='trial_done', updated_at=NOW() WHERE id=$1 AND stage IN ('new','contacted','trial_scheduled')`, it.LeadID)
			}
			changed++
		}
		if req.Topic != nil {
			tx.Exec(ctx, `UPDATE lessons SET topic=NULLIF($2,'') WHERE id=$1`, id, strings.TrimSpace(*req.Topic))
		}
		if req.Homework != nil {
			if len([]rune(*req.Homework)) > 5000 {
				return (&httpx.Validation{Fields: []httpx.FieldError{{Field: "homework", Code: "TOO_LONG"}}}).Err()
			}
			tx.Exec(ctx, `UPDATE lessons SET homework=NULLIF($2,'') WHERE id=$1`, id, strings.TrimSpace(*req.Homework))
		}
		if req.KtpTopicID != nil {
			if _, err := tx.Exec(ctx, `UPDATE lessons SET ktp_topic_id=NULLIF($2,'')::uuid WHERE id=$1`, id, *req.KtpTopicID); err != nil {
				return err
			}
		}
		// баллы за тесты урока — только у пришедших; при закрытии проверяются обязательные тесты
		if req.Tests != nil {
			present := map[string]bool{}
			rows, err := tx.Query(ctx, `SELECT student_id::text FROM attendance WHERE lesson_id=$1 AND student_id IS NOT NULL AND status IN ('present','late','trial')`, id)
			if err != nil {
				return err
			}
			for rows.Next() {
				var sid string
				rows.Scan(&sid)
				present[sid] = true
			}
			rows.Close()
			loc, _ := time.LoadLocation(tz)
			if loc == nil {
				loc = time.UTC
			}
			if err := exam.SaveLesson(ctx, tx, id, groupID, courseID, starts.In(loc).Format("2006-01-02"), teacherID, p.UserID,
				req.Tests, present, req.Close && status != "done"); err != nil {
				return err
			}
		}
		if req.Close && status != "done" {
			// закрытие занятия — триггер списаний, начисления зарплаты и уведомлений (ТЗ 12.5)
			if _, err := tx.Exec(ctx, `UPDATE lessons SET status='done', closed_at=NOW(), closed_by=$2 WHERE id=$1`, id, p.UserID); err != nil {
				return err
			}
			status = "done"
			database.Publish(ctx, tx, "lesson.closed", map[string]any{"lesson_id": id})
			// родителям — итог урока: тема, ДЗ и результаты ребёнка
			rows, err := tx.Query(ctx, `SELECT DISTINCT student_id::text FROM attendance WHERE lesson_id=$1 AND student_id IS NOT NULL`, id)
			if err != nil {
				return err
			}
			students, err := pgx.CollectRows(rows, pgx.RowTo[string])
			if err != nil {
				return err
			}
			for _, sid := range students {
				database.Publish(ctx, tx, "lesson.report", map[string]any{"lesson_id": id, "student_id": sid})
			}
		}
		if status == "done" {
			charged, err := ApplyCharges(ctx, tx, id, p.UserID)
			if err != nil {
				return err
			}
			result["charges"] = charged
			// начисление преподавателю за урок пересчитывается по текущим отметкам
			if err := payroll.Recalc(ctx, tx, id, false); err != nil {
				return err
			}
		}
		result["changed"] = changed
		result["status"] = status
		return nil
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, result)
}

type chargeResult struct {
	StudentID string `json:"student_id"`
	Action    string `json:"action"` // subscription, reversal, lesson_charge, lesson_refund
}

// ApplyCharges приводит списания по занятию в соответствие с текущими отметками и правилами.
// Изменение статуса пересчитывается корректирующей операцией, а не перезаписью (ТЗ 12.6).
func ApplyCharges(ctx context.Context, tx pgx.Tx, lessonID, userID string) ([]chargeResult, error) {
	rules := map[string]bool{}
	rows, err := tx.Query(ctx, `SELECT status, charge FROM attendance_charge_rules`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var s string
		var c bool
		rows.Scan(&s, &c)
		rules[s] = c
	}
	rows.Close()
	var excusedLimit int
	tx.QueryRow(ctx, `SELECT coalesce((settings->>'excused_limit')::int, 0) FROM tenants WHERE id = app_current_tenant()`).Scan(&excusedLimit)

	var courseID, tz string
	var starts time.Time
	var pricePerLesson int64
	if err := tx.QueryRow(ctx, `SELECT g.course_id, b.timezone, l.starts_at, CASE WHEN c.lessons_count > 0 THEN c.price_tiyn / c.lessons_count ELSE 0 END
		FROM lessons l JOIN groups g ON g.id=l.group_id JOIN courses c ON c.id=g.course_id JOIN branches b ON b.id=l.branch_id WHERE l.id=$1`, lessonID).
		Scan(&courseID, &tz, &starts, &pricePerLesson); err != nil {
		return nil, err
	}
	loc, _ := time.LoadLocation(tz)
	lessonDate := starts.In(loc).Format("2006-01-02")

	arows, err := tx.Query(ctx, `SELECT a.student_id::text, a.status, EXISTS (SELECT 1 FROM lesson_guests g WHERE g.lesson_id=a.lesson_id AND g.student_id=a.student_id AND g.is_trial)
		FROM attendance a WHERE a.lesson_id=$1 AND a.student_id IS NOT NULL`, lessonID)
	if err != nil {
		return nil, err
	}
	type att struct {
		student, status string
		trial           bool
	}
	var list []att
	for arows.Next() {
		var a att
		arows.Scan(&a.student, &a.status, &a.trial)
		list = append(list, a)
	}
	arows.Close()

	out := []chargeResult{}
	for _, a := range list {
		want := rules[a.status] && !a.trial && a.status != "trial"
		if a.status == "excused" && !want && excusedLimit > 0 {
			// лимит уважительных пропусков в месяц, после которого пропуск списывается
			var cnt int
			tx.QueryRow(ctx, `SELECT count(*) FROM attendance x JOIN lessons l ON l.id=x.lesson_id WHERE x.student_id=$1 AND x.status='excused'
				AND date_trunc('month', l.starts_at)=date_trunc('month', $2::timestamptz) AND l.starts_at <= $2`, a.student, starts).Scan(&cnt)
			want = cnt > excusedLimit
		}
		// во время заморозки абонемента занятия не списываются
		var frozen bool
		tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM subscription_freezes f JOIN subscriptions s ON s.id=f.subscription_id
			WHERE s.student_id=$1 AND $2::date BETWEEN f.starts_on AND f.ends_on)`, a.student, lessonDate).Scan(&frozen)
		if frozen {
			want = false
		}
		var usage int
		tx.QueryRow(ctx, `SELECT coalesce(sum(qty),0) FROM subscription_usages WHERE lesson_id=$1 AND student_id=$2`, lessonID, a.student).Scan(&usage)
		var lessonCharge int64
		tx.QueryRow(ctx, `SELECT coalesce(sum(amount_tiyn),0) FROM ledger_entries WHERE ref_type='lesson' AND ref_id=$1 AND student_id=$2`, lessonID, a.student).Scan(&lessonCharge)
		charged := usage > 0 || lessonCharge < 0

		switch {
		case want && !charged:
			subID, err := pickSubscription(ctx, tx, a.student, courseID, lessonDate)
			if err != nil {
				return nil, err
			}
			if subID != "" {
				if _, err := tx.Exec(ctx, `INSERT INTO subscription_usages (tenant_id, subscription_id, lesson_id, student_id, qty) VALUES (app_current_tenant(), $1, $2, $3, 1)`,
					subID, lessonID, a.student); err != nil {
					return nil, err
				}
				if err := afterUsage(ctx, tx, subID, a.student); err != nil {
					return nil, err
				}
				out = append(out, chargeResult{a.student, "subscription"})
			} else if pricePerLesson > 0 {
				// без абонемента — разовое начисление по цене занятия курса (уходит в долг в пределах «кредита»)
				if err := finance.Post(ctx, tx, a.student, "charge", -pricePerLesson, "lesson", &lessonID, "Занятие "+lessonDate+" без абонемента", userID); err != nil {
					return nil, err
				}
				out = append(out, chargeResult{a.student, "lesson_charge"})
			}
		case !want && charged:
			if usage > 0 {
				var subID string
				tx.QueryRow(ctx, `SELECT subscription_id FROM subscription_usages WHERE lesson_id=$1 AND student_id=$2 AND qty > 0 ORDER BY id DESC LIMIT 1`, lessonID, a.student).Scan(&subID)
				if _, err := tx.Exec(ctx, `INSERT INTO subscription_usages (tenant_id, subscription_id, lesson_id, student_id, qty) VALUES (app_current_tenant(), $1, $2, $3, -1)`,
					subID, lessonID, a.student); err != nil {
					return nil, err
				}
				tx.Exec(ctx, `UPDATE subscriptions SET status='active' WHERE id=$1 AND status='used'`, subID)
				out = append(out, chargeResult{a.student, "reversal"})
			}
			if lessonCharge < 0 {
				if err := finance.Post(ctx, tx, a.student, "adjustment", -lessonCharge, "lesson", &lessonID, "Корректировка: занятие "+lessonDate+" не списывается", userID); err != nil {
					return nil, err
				}
				out = append(out, chargeResult{a.student, "lesson_refund"})
			}
		}
	}
	return out, nil
}

// pickSubscription: действующий абонемент на дату занятия, подходящий по курсу, с остатком занятий.
func pickSubscription(ctx context.Context, tx pgx.Tx, studentID, courseID, date string) (string, error) {
	var id string
	err := tx.QueryRow(ctx, `SELECT s.id FROM subscriptions s
		WHERE s.student_id=$1 AND s.status IN ('active','frozen') AND (s.course_id IS NULL OR s.course_id=$2)
		AND s.starts_on <= $3::date AND (s.ends_on IS NULL OR s.ends_on >= $3::date)
		AND (s.lessons_total IS NULL OR s.lessons_total > coalesce((SELECT sum(qty) FROM subscription_usages u WHERE u.subscription_id=s.id),0))
		ORDER BY (s.course_id IS NOT NULL) DESC, s.starts_on LIMIT 1`, studentID, courseID, date).Scan(&id)
	if err == pgx.ErrNoRows {
		return "", nil
	}
	return id, err
}

func afterUsage(ctx context.Context, tx pgx.Tx, subID, studentID string) error {
	var total *int
	var used int
	tx.QueryRow(ctx, `SELECT lessons_total, coalesce((SELECT sum(qty) FROM subscription_usages u WHERE u.subscription_id=s.id),0) FROM subscriptions s WHERE id=$1`, subID).Scan(&total, &used)
	if total == nil {
		return nil
	}
	left := *total - used
	if left <= 0 {
		if _, err := tx.Exec(ctx, `UPDATE subscriptions SET status='used' WHERE id=$1`, subID); err != nil {
			return err
		}
	}
	if left <= 2 {
		return database.Publish(ctx, tx, "subscription.expiring", map[string]any{"subscription_id": subID, "student_id": studentID, "lessons_left": left})
	}
	return nil
}

func (h *Handler) history(w http.ResponseWriter, r *http.Request) error {
	out := []map[string]any{}
	err := h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `SELECT h.changed_at, h.old_status, h.new_status, h.reason, coalesce(u.full_name,'Система'), s.full_name, g.name, l.starts_at
			FROM attendance_history h JOIN lessons l ON l.id=h.lesson_id JOIN groups g ON g.id=l.group_id
			LEFT JOIN students s ON s.id=h.student_id LEFT JOIN users u ON u.id=h.changed_by
			WHERE ($1='' OR h.lesson_id::text=$1) AND ($2='' OR h.student_id::text=$2) AND h.old_status IS NOT NULL
			ORDER BY h.changed_at DESC LIMIT 200`, r.URL.Query().Get("lesson_id"), r.URL.Query().Get("student_id"))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var at, lessonAt time.Time
			var old, reason, student *string
			var nw, user, group string
			rows.Scan(&at, &old, &nw, &reason, &user, &student, &group, &lessonAt)
			out = append(out, map[string]any{"changed_at": at, "old": old, "new": nw, "reason": reason, "user": user, "student": student, "group": group, "lesson_at": lessonAt})
		}
		return rows.Err()
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, out)
}

// report — посещаемость по группам за период.
func (h *Handler) report(w http.ResponseWriter, r *http.Request) error {
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	if from == "" {
		from = time.Now().AddDate(0, 0, -30).Format("2006-01-02")
	}
	if to == "" {
		to = time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	}
	p := auth.FromContext(r.Context())
	out := []map[string]any{}
	err := h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `SELECT g.id, g.name, coalesce(e.full_name,''),
			count(DISTINCT l.id) FILTER (WHERE l.status='done'),
			count(DISTINCT l.id) FILTER (WHERE l.status='planned' AND l.ends_at < NOW()),
			count(a.id) FILTER (WHERE a.status IN ('present','late')), count(a.id) FILTER (WHERE a.status='absent'),
			count(a.id) FILTER (WHERE a.status='excused'), count(a.id) FILTER (WHERE a.status <> 'trial')
			FROM lessons l JOIN groups g ON g.id=l.group_id LEFT JOIN employees e ON e.id=g.teacher_id
			LEFT JOIN attendance a ON a.lesson_id=l.id
			WHERE l.starts_at >= $1::date AND l.starts_at < $2::date AND l.status<>'cancelled' AND ($3::uuid[] IS NULL OR l.branch_id = ANY($3))
			GROUP BY g.id, g.name, e.full_name ORDER BY g.name`, from, to, p.BranchFilter())
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id, name, teacher string
			var done, unmarked, present, absent, excused, total int
			rows.Scan(&id, &name, &teacher, &done, &unmarked, &present, &absent, &excused, &total)
			var rate *float64
			if total > 0 {
				v := float64(present) * 100 / float64(total)
				rate = &v
			}
			out = append(out, map[string]any{"group_id": id, "group": name, "teacher": teacher, "lessons_done": done, "lessons_unmarked": unmarked,
				"present": present, "absent": absent, "excused": excused, "rate": rate})
		}
		return rows.Err()
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, out)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

var _ = fmt.Sprint
