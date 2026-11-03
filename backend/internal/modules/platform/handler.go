// Пакет platform — панель SaaS Super Admin. Персональные данные учеников здесь не показываются — только агрегаты (ТЗ 2.2).
package platform

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"emp/internal/platform/auth"
	"emp/internal/platform/httpx"
	"emp/internal/platform/database"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

type Handler struct{ DB *database.DB }

func (h *Handler) Routes(r chi.Router) {
	r.Get("/platform/overview", httpx.Handler(h.overview).ServeHTTP)
	r.Get("/platform/organizations", httpx.Handler(h.orgs).ServeHTTP)
	r.Get("/platform/organizations/{id}", httpx.Handler(h.org).ServeHTTP)
	r.Post("/platform/organizations/{id}/plan", httpx.Handler(h.changePlan).ServeHTTP)
	r.Post("/platform/organizations/{id}/extend", httpx.Handler(h.extend).ServeHTTP)
	r.Post("/platform/organizations/{id}/status", httpx.Handler(h.setStatus).ServeHTTP)
	r.Get("/platform/plans", httpx.Handler(h.plans).ServeHTTP)
	r.Get("/platform/holidays", httpx.Handler(h.holidays).ServeHTTP)
	r.Post("/platform/holidays", httpx.Handler(h.addHoliday).ServeHTTP)
	r.Get("/platform/system", httpx.Handler(h.system).ServeHTTP)
}

type orgRow struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	City           *string   `json:"city"`
	CenterType     string    `json:"center_type"`
	Branches       int       `json:"branches"`
	CreatedAt      time.Time `json:"created_at"`
	Plan           string    `json:"plan"`
	PlanName       string    `json:"plan_name"`
	PriceTiyn      int64     `json:"price_tiyn"`
	Status         string    `json:"status"`
	ActiveStudents int       `json:"active_students"`
	MaxStudents    *int      `json:"max_students"`
	LastActivity   *time.Time `json:"last_activity"`
	Activity       []int     `json:"activity"`
	Health         string    `json:"health"`
	HealthReason   string    `json:"health_reason"`
	PaidUntil      *time.Time `json:"paid_until"`
	TrialEndsAt    *time.Time `json:"trial_ends_at"`
	OverLimitSince *time.Time `json:"over_limit_since"`
	MarkedRate     *float64  `json:"marked_rate"`
	TelegramRate   *float64  `json:"telegram_rate"`
}

const orgSel = `SELECT t.id, t.name, t.city, t.center_type, (SELECT count(*) FROM branches b WHERE b.tenant_id=t.id), t.created_at, t.plan_code, sp.name, sp.price_tiyn, t.status,
	(SELECT count(*) FROM students s WHERE s.tenant_id=t.id AND s.status IN ('active','registration') AND s.archived_at IS NULL), sp.max_students,
	(SELECT max(created_at) FROM audit_logs a WHERE a.tenant_id=t.id AND a.user_id IS NOT NULL),
	ARRAY(SELECT (SELECT count(*) FROM audit_logs a WHERE a.tenant_id=t.id AND a.user_id IS NOT NULL AND a.created_at >= d AND a.created_at < d + interval '1 day')::int
		FROM generate_series(CURRENT_DATE - 13, CURRENT_DATE, interval '1 day') d),
	t.paid_until, t.trial_ends_at, t.over_limit_since,
	(SELECT round(100.0*count(*) FILTER (WHERE l.status='done')/NULLIF(count(*),0)) FROM lessons l WHERE l.tenant_id=t.id AND l.ends_at < NOW() AND l.ends_at > NOW() - interval '14 days' AND l.status<>'cancelled'),
	(SELECT round(100.0*count(*) FILTER (WHERE p.telegram_chat_id IS NOT NULL)/NULLIF(count(*),0)) FROM parents p WHERE p.tenant_id=t.id)
	FROM tenants t JOIN subscription_plans sp ON sp.code=t.plan_code`

func scanOrgs(rows pgx.Rows) ([]orgRow, error) {
	defer rows.Close()
	out := []orgRow{}
	for rows.Next() {
		var o orgRow
		if err := rows.Scan(&o.ID, &o.Name, &o.City, &o.CenterType, &o.Branches, &o.CreatedAt, &o.Plan, &o.PlanName, &o.PriceTiyn, &o.Status, &o.ActiveStudents, &o.MaxStudents,
			&o.LastActivity, &o.Activity, &o.PaidUntil, &o.TrialEndsAt, &o.OverLimitSince, &o.MarkedRate, &o.TelegramRate); err != nil {
			return nil, err
		}
		o.Health, o.HealthReason = health(o)
		if o.MaxStudents != nil && o.ActiveStudents > *o.MaxStudents && o.Status == "active" {
			o.Status = "over_limit"
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// health — эвристика риска оттока по активности и внедрению.
func health(o orgRow) (string, string) {
	switch {
	case o.Status == "suspended":
		return "risk", "приостановлена"
	case o.LastActivity == nil || time.Since(*o.LastActivity) > 7*24*time.Hour:
		days := 0
		if o.LastActivity != nil {
			days = int(time.Since(*o.LastActivity).Hours() / 24)
		}
		if days == 0 {
			return "risk", "нет активности"
		}
		return "risk", "нет входов " + itoa(days) + " дн."
	case o.Status == "past_due":
		return "risk", "просрочка оплаты"
	case o.Status == "trial" && o.ActiveStudents == 0:
		return "risk", "импорт не сделан"
	case o.MarkedRate != nil && *o.MarkedRate < 80:
		return "attention", "отмечено " + itoa(int(*o.MarkedRate)) + "% занятий"
	case o.TelegramRate == nil || *o.TelegramRate == 0:
		return "attention", "не подключён Telegram-бот"
	case o.MaxStudents != nil && o.ActiveStudents > *o.MaxStudents:
		return "good", "лимит превышен"
	}
	if o.MarkedRate != nil {
		return "good", "посещаемость отмечена в " + itoa(int(*o.MarkedRate)) + "% занятий"
	}
	return "good", "все модули в работе"
}

func itoa(i int) string { return strconv.Itoa(i) }

func (h *Handler) orgs(w http.ResponseWriter, r *http.Request) error {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	rows, err := h.DB.Sys.Query(r.Context(), orgSel+` WHERE ($1='' OR t.name ILIKE '%'||$1||'%' OR t.bin=$1
		OR EXISTS (SELECT 1 FROM users u JOIN user_roles ur ON ur.user_id=u.id JOIN roles ro ON ro.id=ur.role_id AND ro.code='owner' WHERE u.tenant_id=t.id AND u.email ILIKE '%'||$1||'%'))
		ORDER BY t.created_at`, q)
	if err != nil {
		return err
	}
	list, err := scanOrgs(rows)
	if err != nil {
		return err
	}
	status, plan, hl := r.URL.Query().Get("status"), r.URL.Query().Get("plan"), r.URL.Query().Get("health")
	counts := map[string]int{"all": len(list)}
	out := []orgRow{}
	for _, o := range list {
		counts[o.Status]++
		if (status == "" || o.Status == status) && (plan == "" || o.Plan == plan) && (hl == "" || o.Health == hl) {
			out = append(out, o)
		}
	}
	return httpx.OKMeta(w, out, map[string]any{"counts": counts})
}

func (h *Handler) overview(w http.ResponseWriter, r *http.Request) error {
	rows, err := h.DB.Sys.Query(r.Context(), orgSel)
	if err != nil {
		return err
	}
	list, err := scanOrgs(rows)
	if err != nil {
		return err
	}
	var paying, trial, pastDue, risk, overLimit, trialEndingWeek, trialEndingNoImport, newPaying int
	var mrr, pastDueSum int64
	monthAgo := time.Now().AddDate(0, -1, 0)
	for _, o := range list {
		switch o.Status {
		case "active", "over_limit":
			paying++
			mrr += o.PriceTiyn
			if o.CreatedAt.After(monthAgo) {
				newPaying++
			}
		case "trial":
			trial++
			if o.TrialEndsAt != nil && time.Until(*o.TrialEndsAt) < 7*24*time.Hour {
				trialEndingWeek++
				if o.ActiveStudents == 0 {
					trialEndingNoImport++
				}
			}
		case "past_due":
			pastDue++
			pastDueSum += o.PriceTiyn
		}
		if o.Status == "over_limit" {
			overLimit++
		}
		if o.Health == "risk" {
			risk++
		}
	}
	var converted, trialsTotal int
	h.DB.Sys.QueryRow(r.Context(), `SELECT count(*) FILTER (WHERE status IN ('active','past_due')), count(*) FROM tenants WHERE trial_ends_at IS NOT NULL`).Scan(&converted, &trialsTotal)
	var conv *float64
	if trialsTotal > 0 {
		v := float64(converted) * 100 / float64(trialsTotal)
		conv = &v
	}
	return httpx.OK(w, map[string]any{"paying": paying, "new_paying": newPaying, "trial": trial, "trial_conversion": conv, "mrr_tiyn": mrr,
		"past_due": pastDue, "past_due_tiyn": pastDueSum, "risk": risk, "over_limit": overLimit,
		"trial_ending_week": trialEndingWeek, "trial_ending_no_import": trialEndingNoImport})
}

func (h *Handler) org(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	rows, err := h.DB.Sys.Query(ctx, orgSel+" WHERE t.id=$1", id)
	if err != nil {
		return err
	}
	list, err := scanOrgs(rows)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		return httpx.NotFound("ORG_NOT_FOUND")
	}
	out := map[string]any{"org": list[0]}
	var bin *string
	var cycle string
	h.DB.Sys.QueryRow(ctx, `SELECT bin, billing_cycle FROM tenants WHERE id=$1`, id).Scan(&bin, &cycle)
	out["bin"], out["billing_cycle"] = bin, cycle

	var staff, maxStaff, fields, maxFields, maxBranches *int
	h.DB.Sys.QueryRow(ctx, `SELECT (SELECT count(*) FROM users WHERE tenant_id=t.id AND status='active')::int, sp.max_staff,
		(SELECT count(*) FROM custom_field_definitions WHERE tenant_id=t.id AND archived_at IS NULL)::int, sp.max_custom_fields, sp.max_branches
		FROM tenants t JOIN subscription_plans sp ON sp.code=t.plan_code WHERE t.id=$1`, id).Scan(&staff, &maxStaff, &fields, &maxFields, &maxBranches)
	var smsMonth int
	h.DB.Sys.QueryRow(ctx, `SELECT count(*) FROM notification_logs WHERE tenant_id=$1 AND channel='sms' AND created_at >= date_trunc('month', NOW())`, id).Scan(&smsMonth)
	out["limits"] = map[string]any{"students": list[0].ActiveStudents, "max_students": list[0].MaxStudents, "branches": list[0].Branches, "max_branches": maxBranches,
		"staff": staff, "max_staff": maxStaff, "custom_fields": fields, "max_custom_fields": maxFields, "sms": smsMonth}

	// активность за 30 дней: активные сотрудники и доля занятий с отметкой
	arows, err := h.DB.Sys.Query(ctx, `SELECT d::date,
		(SELECT count(DISTINCT user_id) FROM audit_logs a WHERE a.tenant_id=$1 AND a.user_id IS NOT NULL AND a.created_at >= d AND a.created_at < d + interval '1 day'),
		(SELECT round(100.0*count(*) FILTER (WHERE l.status='done')/NULLIF(count(*),0)) FROM lessons l WHERE l.tenant_id=$1 AND l.starts_at >= d AND l.starts_at < d + interval '1 day' AND l.status<>'cancelled')
		FROM generate_series(CURRENT_DATE - 29, CURRENT_DATE, interval '1 day') d ORDER BY d`, id)
	if err != nil {
		return err
	}
	activity := []map[string]any{}
	for arows.Next() {
		var d time.Time
		var users int
		var marked *float64
		arows.Scan(&d, &users, &marked)
		activity = append(activity, map[string]any{"day": d.Format("2006-01-02"), "active_staff": users, "marked_rate": marked})
	}
	arows.Close()
	out["activity"] = activity

	var courses, groups, schedules, payments, students int
	h.DB.Sys.QueryRow(ctx, `SELECT (SELECT count(*) FROM courses WHERE tenant_id=$1), (SELECT count(*) FROM groups WHERE tenant_id=$1 AND type='group'),
		(SELECT count(*) FROM schedules WHERE tenant_id=$1), (SELECT count(*) FROM payments WHERE tenant_id=$1), (SELECT count(*) FROM students WHERE tenant_id=$1)`, id).
		Scan(&courses, &groups, &schedules, &payments, &students)
	out["adoption"] = map[string]any{"branches": list[0].Branches, "courses": courses, "students": students, "groups": groups, "schedules": schedules,
		"marked_rate": list[0].MarkedRate, "payments": payments, "telegram_rate": list[0].TelegramRate, "custom_fields": fields}

	orows, err := h.DB.Sys.Query(ctx, `SELECT u.full_name, u.email, coalesce(u.phone,''), r.name, u.last_login_at, u.mfa_enabled FROM users u
		JOIN user_roles ur ON ur.user_id=u.id JOIN roles r ON r.id=ur.role_id WHERE u.tenant_id=$1 AND r.code IN ('owner','org_admin') ORDER BY r.code DESC`, id)
	if err != nil {
		return err
	}
	contacts := []map[string]any{}
	for orows.Next() {
		var name, email, phone, role string
		var last *time.Time
		var mfa bool
		orows.Scan(&name, &email, &phone, &role, &last, &mfa)
		contacts = append(contacts, map[string]any{"full_name": name, "email": email, "phone": phone, "role": role, "last_login_at": last, "mfa": mfa})
	}
	orows.Close()
	out["contacts"] = contacts

	var sent7, failed24 int
	h.DB.Sys.QueryRow(ctx, `SELECT count(*) FILTER (WHERE status='sent' AND created_at > NOW() - interval '7 days'), count(*) FILTER (WHERE status='failed' AND created_at > NOW() - interval '1 day')
		FROM notification_logs WHERE tenant_id=$1 AND channel='telegram'`, id).Scan(&sent7, &failed24)
	out["integrations"] = map[string]any{"telegram_sent_7d": sent7, "telegram_failed_24h": failed24}

	var paidTotal int64
	var paidCount int
	h.DB.Sys.QueryRow(ctx, `SELECT coalesce(sum(amount_tiyn),0), count(*) FROM tenant_billing_payments WHERE tenant_id=$1`, id).Scan(&paidTotal, &paidCount)
	out["billing"] = map[string]any{"total_tiyn": paidTotal, "count": paidCount}

	// журнал платформы: только административные события, без содержимого карточек
	jrows, err := h.DB.Sys.Query(ctx, `SELECT a.action, a.entity_type, a.created_at, coalesce(u.full_name,'Система') FROM audit_logs a LEFT JOIN users u ON u.id=a.user_id
		WHERE a.tenant_id=$1 AND a.entity_type IN ('tenant','branch','user','role','custom_field','organization','platform') AND a.action <> 'login' ORDER BY a.created_at DESC LIMIT 15`, id)
	if err != nil {
		return err
	}
	journal := []map[string]any{}
	for jrows.Next() {
		var action, et, user string
		var at time.Time
		jrows.Scan(&action, &et, &at, &user)
		journal = append(journal, map[string]any{"action": action, "entity_type": et, "created_at": at, "user": user})
	}
	jrows.Close()
	out["journal"] = journal
	return httpx.OK(w, out)
}

func (h *Handler) audit(r *http.Request, tenantID, action string, values map[string]any) {
	p := auth.FromContext(r.Context())
	h.DB.Sys.Exec(r.Context(), `INSERT INTO audit_logs (tenant_id, user_id, entity_type, entity_id, action, new_values) VALUES ($1, NULL, 'platform', $1, $2, $3)`,
		tenantID, action, map[string]any{"by": p.UserID, "values": values})
}

func (h *Handler) changePlan(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Plan  string `json:"plan"`
		Cycle string `json:"cycle"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	id := chi.URLParam(r, "id")
	tag, err := h.DB.Sys.Exec(r.Context(), `UPDATE tenants SET plan_code=$2, billing_cycle=COALESCE(NULLIF($3,''), billing_cycle), over_limit_since=NULL WHERE id=$1`, id, req.Plan, req.Cycle)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.NotFound("ORG_NOT_FOUND")
	}
	h.audit(r, id, "plan_changed", map[string]any{"plan": req.Plan})
	return httpx.NoContent(w)
}

// extend — ручное продление подписки (например, после оплаты счёта через Kaspi).
func (h *Handler) extend(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Months     int    `json:"months"`
		AmountTiyn int64  `json:"amount_tiyn"`
		Method     string `json:"method"`
		Comment    string `json:"comment"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.Months <= 0 || req.Months > 24 {
		return httpx.BadRequest("VALIDATION_FAILED", "Укажите количество месяцев")
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	var until time.Time
	err := pgx.BeginFunc(ctx, h.DB.Sys, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `UPDATE tenants SET paid_until = GREATEST(coalesce(paid_until, CURRENT_DATE), CURRENT_DATE) + make_interval(months => $2),
			status = 'active' WHERE id=$1 RETURNING paid_until`, id, req.Months).Scan(&until); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO tenant_billing_payments (tenant_id, amount_tiyn, method, period_to, comment) VALUES ($1,$2,$3,$4,$5)`,
			id, req.AmountTiyn, firstNonEmpty(req.Method, "manual"), until, req.Comment)
		return err
	})
	if err != nil {
		return err
	}
	h.audit(r, id, "subscription_extended", map[string]any{"months": req.Months, "until": until.Format("2006-01-02")})
	return httpx.OK(w, map[string]any{"paid_until": until})
}

func (h *Handler) setStatus(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.Status != "active" && req.Status != "suspended" && req.Status != "past_due" && req.Status != "trial" {
		return httpx.BadRequest("INVALID_STATUS", "Неизвестный статус")
	}
	id := chi.URLParam(r, "id")
	if _, err := h.DB.Sys.Exec(r.Context(), `UPDATE tenants SET status=$2 WHERE id=$1`, id, req.Status); err != nil {
		return err
	}
	if req.Status == "suspended" {
		// данные не удаляются; пользователи организации выходят из системы
		h.DB.Sys.Exec(r.Context(), `UPDATE sessions SET revoked_at=NOW() WHERE revoked_at IS NULL AND user_id IN (SELECT id FROM users WHERE tenant_id=$1)`, id)
	}
	h.audit(r, id, "status_changed", map[string]any{"status": req.Status, "reason": req.Reason})
	return httpx.NoContent(w)
}

func (h *Handler) plans(w http.ResponseWriter, r *http.Request) error {
	rows, err := h.DB.Sys.Query(r.Context(), `SELECT code, name, price_tiyn, max_students, max_branches, max_staff, max_custom_fields, storage_gb, sms_per_month,
		(SELECT count(*) FROM tenants WHERE plan_code=sp.code) FROM subscription_plans sp ORDER BY sort_order`)
	if err != nil {
		return err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var code, name string
		var price int64
		var ms, mb, mst *int
		var mcf, gb, sms, orgs int
		rows.Scan(&code, &name, &price, &ms, &mb, &mst, &mcf, &gb, &sms, &orgs)
		out = append(out, map[string]any{"code": code, "name": name, "price_tiyn": price, "max_students": ms, "max_branches": mb, "max_staff": mst,
			"max_custom_fields": mcf, "storage_gb": gb, "sms_per_month": sms, "organizations": orgs})
	}
	return httpx.OK(w, out)
}

func (h *Handler) holidays(w http.ResponseWriter, r *http.Request) error {
	rows, err := h.DB.Sys.Query(r.Context(), `SELECT id, day, name, is_workday FROM calendar_holidays WHERE tenant_id IS NULL AND day >= date_trunc('year', NOW()) ORDER BY day`)
	if err != nil {
		return err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, name string
		var d time.Time
		var wd bool
		rows.Scan(&id, &d, &name, &wd)
		out = append(out, map[string]any{"id": id, "day": d.Format("2006-01-02"), "name": name, "is_workday": wd})
	}
	return httpx.OK(w, out)
}

func (h *Handler) addHoliday(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Day       string `json:"day"`
		Name      string `json:"name"`
		IsWorkday bool   `json:"is_workday"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if _, err := time.Parse("2006-01-02", req.Day); err != nil || strings.TrimSpace(req.Name) == "" {
		return httpx.BadRequest("VALIDATION_FAILED", "Укажите дату и название")
	}
	_, err := h.DB.Sys.Exec(r.Context(), `INSERT INTO calendar_holidays (day, name, is_workday) VALUES ($1,$2,$3)`, req.Day, req.Name, req.IsWorkday)
	if err != nil {
		return err
	}
	return httpx.NoContent(w)
}

func (h *Handler) system(w http.ResponseWriter, r *http.Request) error {
	var backlog, failedEvents, failedNotif int
	var dbSize string
	ctx := r.Context()
	h.DB.Sys.QueryRow(ctx, `SELECT count(*) FILTER (WHERE processed_at IS NULL AND attempts < 10), count(*) FILTER (WHERE processed_at IS NULL AND attempts >= 10) FROM outbox_events`).Scan(&backlog, &failedEvents)
	h.DB.Sys.QueryRow(ctx, `SELECT count(*) FROM notification_logs WHERE status='failed' AND created_at > NOW() - interval '1 day'`).Scan(&failedNotif)
	h.DB.Sys.QueryRow(ctx, `SELECT pg_size_pretty(pg_database_size(current_database()))`).Scan(&dbSize)
	stat := h.DB.App.Stat()
	return httpx.OK(w, map[string]any{"outbox_backlog": backlog, "outbox_failed": failedEvents, "notifications_failed_24h": failedNotif, "db_size": dbSize,
		"db_pool": map[string]any{"total": stat.TotalConns(), "idle": stat.IdleConns(), "acquired": stat.AcquiredConns()}})
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
