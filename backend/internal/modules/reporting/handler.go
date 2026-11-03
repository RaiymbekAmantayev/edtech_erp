// Пакет reporting — главная панель, базовые отчёты и глобальный поиск.
package reporting

import (
	"net/http"
	"strings"
	"time"
	"unicode"

	"emp/internal/modules/schedule"
	"emp/internal/platform/auth"
	"emp/internal/platform/database"
	"emp/internal/platform/httpx"
	mw "emp/internal/platform/middleware"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

type Handler struct{ DB *database.DB }

func (h *Handler) Routes(r chi.Router) {
	r.Get("/dashboard", httpx.Handler(h.dashboard).ServeHTTP)
	r.Get("/search", httpx.Handler(h.search).ServeHTTP)
	r.With(mw.Perm("reports.view")).Get("/reports/students", httpx.Handler(h.studentsReport).ServeHTTP)
	r.With(mw.Perm("audit.view")).Get("/audit", httpx.Handler(h.audit).ServeHTTP)
}

func (h *Handler) dashboard(w http.ResponseWriter, r *http.Request) error {
	p := auth.FromContext(r.Context())
	ctx := r.Context()
	branch := r.URL.Query().Get("branch_id")
	period := r.URL.Query().Get("period")
	out := map[string]any{}
	err := h.DB.Tx(ctx, func(tx pgx.Tx) error {
		var tz string
		tx.QueryRow(ctx, `SELECT timezone FROM tenants WHERE id = app_current_tenant()`).Scan(&tz)
		loc, err := time.LoadLocation(tz)
		if err != nil {
			loc, _ = time.LoadLocation("Asia/Almaty")
		}
		now := time.Now().In(loc)
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		var from, prevFrom time.Time
		switch period {
		case "today":
			from, prevFrom = today, today.AddDate(0, 0, -1)
		case "week":
			from = today.AddDate(0, 0, -(int(today.Weekday())+6)%7)
			prevFrom = from.AddDate(0, 0, -7)
		default:
			period = "month"
			from = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
			prevFrom = from.AddDate(0, -1, 0)
		}
		elapsed := now.Sub(from)
		prevTo := prevFrom.Add(elapsed)

		bf := p.BranchFilter()
		if branch != "" {
			if !p.CanBranch(branch) {
				return httpx.Forbidden()
			}
			bf = []string{branch}
		}
		out["period"] = map[string]any{"name": period, "from": from, "now": now}
		out["greeting_name"] = strings.Fields(p.FullName + " ")[0]

		if p.Can("students.view") {
			var active, newActive int
			tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE status='active'), count(*) FILTER (WHERE status='active' AND started_at >= $1::date)
				FROM students WHERE archived_at IS NULL AND ($2::uuid[] IS NULL OR branch_id=ANY($2))`, from, bf).Scan(&active, &newActive)
			out["active_students"] = map[string]int{"value": active, "delta": newActive}
		}
		if p.Can("leads.view") {
			var leads, won int
			tx.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE stage='won') FROM leads WHERE created_at >= $1 AND ($2::uuid[] IS NULL OR branch_id IS NULL OR branch_id=ANY($2))`, from, bf).Scan(&leads, &won)
			out["new_leads"] = map[string]int{"value": leads, "won": won}
		}
		if p.Can("attendance.view") {
			var rate *float64
			tx.QueryRow(ctx, `SELECT round(100.0 * count(*) FILTER (WHERE a.status IN ('present','late')) / NULLIF(count(*) FILTER (WHERE a.status<>'trial'),0))
				FROM attendance a JOIN lessons l ON l.id=a.lesson_id WHERE l.starts_at >= NOW() - interval '30 days' AND ($1::uuid[] IS NULL OR l.branch_id=ANY($1))`, bf).Scan(&rate)
			out["attendance_rate"] = rate
		}
		if p.Can("finance.view") {
			var income, prevIncome, debt int64
			var debtors int
			q := `SELECT coalesce(sum(p.amount_tiyn),0) FROM payments p JOIN students s ON s.id=p.student_id WHERE p.paid_at >= $1 AND p.paid_at < $2 AND ($3::uuid[] IS NULL OR s.branch_id=ANY($3))`
			tx.QueryRow(ctx, q, from, now, bf).Scan(&income)
			tx.QueryRow(ctx, q, prevFrom, prevTo, bf).Scan(&prevIncome)
			tx.QueryRow(ctx, `SELECT coalesce(sum(b.balance_tiyn),0), count(*) FROM student_balances b JOIN students s ON s.id=b.student_id
				WHERE b.balance_tiyn < 0 AND s.archived_at IS NULL AND ($1::uuid[] IS NULL OR s.branch_id=ANY($1))`, bf).Scan(&debt, &debtors)
			out["income"] = map[string]int64{"value": income, "previous": prevIncome}
			out["debt"] = map[string]any{"value": -debt, "debtors": debtors}

			// поступления по неделям (6 недель)
			weekStart := today.AddDate(0, 0, -(int(today.Weekday())+6)%7).AddDate(0, 0, -35)
			rows, err := tx.Query(ctx, `SELECT date_trunc('week', p.paid_at AT TIME ZONE $3)::date, sum(p.amount_tiyn) FROM payments p JOIN students s ON s.id=p.student_id
				WHERE p.paid_at >= $1 AND ($2::uuid[] IS NULL OR s.branch_id=ANY($2)) GROUP BY 1 ORDER BY 1`, weekStart, bf, tz)
			if err != nil {
				return err
			}
			sums := map[string]int64{}
			for rows.Next() {
				var d time.Time
				var v int64
				rows.Scan(&d, &v)
				sums[d.Format("2006-01-02")] = v
			}
			rows.Close()
			weeks := []map[string]any{}
			for i := 0; i < 6; i++ {
				d := weekStart.AddDate(0, 0, 7*i)
				weeks = append(weeks, map[string]any{"week": d.Format("2006-01-02"), "amount_tiyn": sums[d.Format("2006-01-02")], "partial": i == 5})
			}
			out["weekly_income"] = weeks
		}

		// занятия сегодня
		q := map[string]string{"from": today.UTC().Format(time.RFC3339), "to": today.AddDate(0, 0, 1).UTC().Format(time.RFC3339), "hide_cancelled": "1"}
		if branch != "" {
			q["branch_id"] = branch
		}
		where, args := schedule.LessonFilter(p, q)
		if p.Can("schedule.view") || p.EmployeeID != "" {
			rows, err := tx.Query(ctx, `SELECT l.id FROM lessons l`+where+` ORDER BY l.starts_at`, args...)
			if err != nil {
				return err
			}
			ids, _ := pgx.CollectRows(rows, pgx.RowTo[string])
			lessons := []schedule.LessonDTO{}
			if len(ids) > 0 {
				lrows, err := tx.Query(ctx, lessonSel+` WHERE l.id = ANY($1::uuid[]) ORDER BY l.starts_at, g.name`, ids)
				if err != nil {
					return err
				}
				if lessons, err = schedule.ScanLessons(lrows); err != nil {
					return err
				}
			}
			out["lessons_today"] = lessons
		}

		// требует внимания
		att := map[string]int{}
		if p.Can("attendance.view") || p.EmployeeID != "" {
			w2, a2 := schedule.LessonFilter(p, map[string]string{"unmarked": "1", "branch_id": branch})
			var n int
			tx.QueryRow(ctx, `SELECT count(*) FROM lessons l`+w2, a2...).Scan(&n)
			att["unmarked_lessons"] = n
		}
		if p.Can("finance.view") {
			var n, f int
			tx.QueryRow(ctx, `SELECT count(*) FROM subscriptions sb JOIN students s ON s.id=sb.student_id WHERE sb.status='active' AND ($1::uuid[] IS NULL OR s.branch_id=ANY($1)) AND (
				sb.ends_on BETWEEN CURRENT_DATE AND CURRENT_DATE + 7
				OR (sb.lessons_total IS NOT NULL AND sb.lessons_total - coalesce((SELECT sum(qty) FROM subscription_usages u WHERE u.subscription_id=sb.id),0) <= 2))`, bf).Scan(&n)
			att["subscriptions_ending"] = n
			tx.QueryRow(ctx, `SELECT count(*) FROM subscription_freezes f WHERE f.ends_on BETWEEN CURRENT_DATE AND CURRENT_DATE + 7`).Scan(&f)
			att["freezes_ending"] = f
		}
		if p.Can("leads.view") {
			var n int
			tx.QueryRow(ctx, `SELECT count(*) FROM leads WHERE stage='new' AND last_contact_at IS NULL AND created_at < NOW() - interval '24 hours'`).Scan(&n)
			att["leads_no_contact"] = n
		}
		if p.Can("schedule.view") {
			var trials, decisions int
			tx.QueryRow(ctx, `SELECT count(*) FROM lesson_guests lg JOIN lessons l ON l.id=lg.lesson_id WHERE lg.is_trial AND l.starts_at >= $1 AND l.starts_at < $2 AND l.status<>'cancelled'`,
				today, today.AddDate(0, 0, 1)).Scan(&trials)
			tx.QueryRow(ctx, `SELECT count(*) FROM lessons WHERE status='needs_decision' AND starts_at >= NOW() - interval '1 day'`).Scan(&decisions)
			att["trials_today"] = trials
			att["holiday_decisions"] = decisions
		}
		out["attention"] = att

		var overSince *time.Time
		var max *int
		var active int
		tx.QueryRow(ctx, `SELECT t.over_limit_since, sp.max_students, (SELECT count(*) FROM students WHERE status IN ('active','registration') AND archived_at IS NULL)
			FROM tenants t JOIN subscription_plans sp ON sp.code=t.plan_code WHERE t.id = app_current_tenant()`).Scan(&overSince, &max, &active)
		if max != nil && active >= *max {
			out["plan_limit"] = map[string]any{"max": *max, "active": active, "since": overSince}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, out)
}

var lessonSel = schedule.LessonSelect()

func (h *Handler) search(w http.ResponseWriter, r *http.Request) error {
	p := auth.FromContext(r.Context())
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	out := []map[string]any{}
	if len([]rune(q)) < 2 {
		return httpx.OK(w, out)
	}
	digits := strings.Map(func(r rune) rune {
		if unicode.IsDigit(r) {
			return r
		}
		return -1
	}, q)
	if len(digits) < 3 {
		digits = "#" // «#» не встречается в телефонах E.164 — поиск по телефону выключен
	}
	err := h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		type src struct {
			perm, sql string
		}
		sources := []src{
			{"students.view", `SELECT 'student', id::text, full_name, coalesce(phone,''), status FROM students WHERE archived_at IS NULL AND ($3::uuid[] IS NULL OR branch_id=ANY($3))
				AND (full_name ILIKE '%'||$1||'%' OR phone LIKE '%'||$2||'%' OR similarity(full_name,$1) > 0.35) ORDER BY similarity(full_name,$1) DESC LIMIT 6`},
			{"parents.view", `SELECT 'parent', id::text, full_name, coalesce(phone,''), '' FROM parents WHERE archived_at IS NULL AND ($3::uuid[] IS NULL OR true)
				AND (full_name ILIKE '%'||$1||'%' OR phone LIKE '%'||$2||'%') LIMIT 4`},
			{"groups.view", `SELECT 'group', id::text, name, '', type FROM groups WHERE status='active' AND ($3::uuid[] IS NULL OR branch_id=ANY($3)) AND name ILIKE '%'||$1||'%' AND $2::text IS NOT NULL LIMIT 4`},
			{"leads.view", `SELECT 'lead', id::text, full_name, phone, stage FROM leads WHERE student_id IS NULL AND ($3::uuid[] IS NULL OR true)
				AND (full_name ILIKE '%'||$1||'%' OR phone LIKE '%'||$2||'%') LIMIT 4`},
		}
		for _, s := range sources {
			if !p.Can(s.perm) {
				continue
			}
			rows, err := tx.Query(r.Context(), s.sql, q, digits, p.BranchFilter())
			if err != nil {
				return err
			}
			for rows.Next() {
				var kind, id, title, sub, status string
				rows.Scan(&kind, &id, &title, &sub, &status)
				out = append(out, map[string]any{"kind": kind, "id": id, "title": title, "subtitle": sub, "status": status})
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, out)
}

// studentsReport — движение учеников по месяцам: новые, выбывшие, активные; источники и причины ухода.
func (h *Handler) studentsReport(w http.ResponseWriter, r *http.Request) error {
	p := auth.FromContext(r.Context())
	out := map[string]any{}
	err := h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		bf := p.BranchFilter()
		rows, err := tx.Query(r.Context(), `SELECT to_char(m, 'YYYY-MM'),
			(SELECT count(*) FROM students s WHERE date_trunc('month', s.started_at)=m AND ($1::uuid[] IS NULL OR s.branch_id=ANY($1))),
			(SELECT count(DISTINCT gs.student_id) FROM group_students gs JOIN students s ON s.id=gs.student_id
				WHERE date_trunc('month', gs.left_at)=m AND gs.leave_reason IS DISTINCT FROM 'transfer' AND ($1::uuid[] IS NULL OR s.branch_id=ANY($1))),
			(SELECT count(*) FROM leads l WHERE date_trunc('month', l.created_at)=m)
			FROM generate_series(date_trunc('month', NOW()) - interval '5 months', date_trunc('month', NOW()), interval '1 month') m ORDER BY m`, bf)
		if err != nil {
			return err
		}
		months := []map[string]any{}
		for rows.Next() {
			var m string
			var joined, left, leads int
			rows.Scan(&m, &joined, &left, &leads)
			months = append(months, map[string]any{"month": m, "joined": joined, "left": left, "leads": leads})
		}
		rows.Close()
		out["months"] = months
		srows, err := tx.Query(r.Context(), `SELECT coalesce(source,'Не указан'), count(*), count(*) FILTER (WHERE stage='won') FROM leads GROUP BY 1 ORDER BY 2 DESC`)
		if err != nil {
			return err
		}
		sources := []map[string]any{}
		for srows.Next() {
			var s string
			var c, won int
			srows.Scan(&s, &c, &won)
			sources = append(sources, map[string]any{"source": s, "leads": c, "won": won})
		}
		srows.Close()
		out["sources"] = sources
		lrows, err := tx.Query(r.Context(), `SELECT coalesce(leave_reason,'Не указана'), count(*) FROM group_students WHERE left_at IS NOT NULL AND leave_reason IS DISTINCT FROM 'transfer' GROUP BY 1 ORDER BY 2 DESC`)
		if err != nil {
			return err
		}
		reasons := []map[string]any{}
		for lrows.Next() {
			var s string
			var c int
			lrows.Scan(&s, &c)
			reasons = append(reasons, map[string]any{"reason": s, "count": c})
		}
		lrows.Close()
		out["leave_reasons"] = reasons
		var byStatus = map[string]int{}
		strows, _ := tx.Query(r.Context(), `SELECT status, count(*) FROM students WHERE ($1::uuid[] IS NULL OR branch_id=ANY($1)) GROUP BY status`, bf)
		for strows.Next() {
			var s string
			var c int
			strows.Scan(&s, &c)
			byStatus[s] = c
		}
		strows.Close()
		out["by_status"] = byStatus
		return nil
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, out)
}

func (h *Handler) audit(w http.ResponseWriter, r *http.Request) error {
	out := []map[string]any{}
	err := h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `SELECT a.id, a.entity_type, a.entity_id, a.action, a.old_values, a.new_values, a.ip_address, a.created_at, coalesce(u.full_name,'Система'), a.impersonated_by IS NOT NULL
			FROM audit_logs a LEFT JOIN users u ON u.id=a.user_id WHERE ($1='' OR a.entity_type=$1) ORDER BY a.id DESC LIMIT 200`, r.URL.Query().Get("entity"))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			var et, action, user string
			var eid, ip *string
			var oldV, newV map[string]any
			var at time.Time
			var imp bool
			rows.Scan(&id, &et, &eid, &action, &oldV, &newV, &ip, &at, &user, &imp)
			out = append(out, map[string]any{"id": id, "entity_type": et, "entity_id": eid, "action": action, "old": oldV, "new": newV, "ip": ip, "created_at": at, "user": user, "impersonated": imp})
		}
		return rows.Err()
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, out)
}
