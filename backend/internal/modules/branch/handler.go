// Пакет branch — организация, филиалы, кабинеты и производственный календарь.
package branch

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"emp/internal/platform/auth"
	"emp/internal/platform/database"
	"emp/internal/platform/httpx"
	mw "emp/internal/platform/middleware"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

type Handler struct{ DB *database.DB }

func (h *Handler) Routes(r chi.Router) {
	r.Get("/branches", httpx.Handler(h.list).ServeHTTP)
	r.Get("/classrooms", httpx.Handler(h.classrooms).ServeHTTP)
	r.Get("/holidays", httpx.Handler(h.holidays).ServeHTTP)
	r.Get("/organization", httpx.Handler(h.org).ServeHTTP)
	r.Group(func(r chi.Router) {
		r.Use(mw.Perm("branches.manage"))
		r.Post("/branches", httpx.Handler(h.create).ServeHTTP)
		r.Patch("/branches/{id}", httpx.Handler(h.update).ServeHTTP)
		r.Post("/classrooms", httpx.Handler(h.createRoom).ServeHTTP)
		r.Patch("/classrooms/{id}", httpx.Handler(h.updateRoom).ServeHTTP)
	})
	r.Group(func(r chi.Router) {
		r.Use(mw.Perm("settings.manage"))
		r.Patch("/organization", httpx.Handler(h.updateOrg).ServeHTTP)
		r.Post("/holidays", httpx.Handler(h.addHoliday).ServeHTTP)
		r.Delete("/holidays/{id}", httpx.Handler(h.deleteHoliday).ServeHTTP)
		r.Get("/attendance-rules", httpx.Handler(h.rules).ServeHTTP)
		r.Put("/attendance-rules", httpx.Handler(h.saveRules).ServeHTTP)
	})
}

func (h *Handler) org(w http.ResponseWriter, r *http.Request) error {
	out := map[string]any{}
	err := h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		var id, name, ctype, tz, locale, plan, status string
		var city, bin *string
		var settings map[string]any
		var trial *time.Time
		if err := tx.QueryRow(r.Context(), `SELECT id, name, center_type, city, bin, timezone, locale, plan_code, status, settings, trial_ends_at FROM tenants WHERE id = app_current_tenant()`).
			Scan(&id, &name, &ctype, &city, &bin, &tz, &locale, &plan, &status, &settings, &trial); err != nil {
			return err
		}
		out = map[string]any{"id": id, "name": name, "center_type": ctype, "city": city, "bin": bin, "timezone": tz, "locale": locale,
			"plan": plan, "status": status, "settings": settings, "trial_ends_at": trial}
		// onboarding-чеклист: филиал → курс → группа → импорт учеников → расписание
		var courses, groups, students, schedules, teachers int
		tx.QueryRow(r.Context(), `SELECT (SELECT count(*) FROM courses), (SELECT count(*) FROM groups), (SELECT count(*) FROM students), (SELECT count(*) FROM schedules), (SELECT count(*) FROM employees WHERE is_teacher)`).
			Scan(&courses, &groups, &students, &schedules, &teachers)
		out["onboarding"] = map[string]bool{"branch": true, "teacher": teachers > 0, "course": courses > 0, "group": groups > 0, "students": students > 0, "schedule": schedules > 0}
		return nil
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, out)
}

func (h *Handler) updateOrg(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Name     *string        `json:"name"`
		City     *string        `json:"city"`
		BIN      *string        `json:"bin"`
		Timezone *string        `json:"timezone"`
		Locale   *string        `json:"locale"`
		Settings map[string]any `json:"settings"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.Timezone != nil {
		if _, err := time.LoadLocation(*req.Timezone); err != nil {
			return httpx.BadRequest("INVALID_TIMEZONE", "Используйте IANA-идентификатор, например Asia/Almaty")
		}
	}
	return h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		ctx := r.Context()
		if req.Name != nil && strings.TrimSpace(*req.Name) != "" {
			tx.Exec(ctx, `UPDATE tenants SET name=$1 WHERE id=app_current_tenant()`, strings.TrimSpace(*req.Name))
		}
		if req.City != nil {
			tx.Exec(ctx, `UPDATE tenants SET city=$1 WHERE id=app_current_tenant()`, *req.City)
		}
		if req.BIN != nil {
			tx.Exec(ctx, `UPDATE tenants SET bin=$1 WHERE id=app_current_tenant()`, *req.BIN)
		}
		if req.Timezone != nil {
			tx.Exec(ctx, `UPDATE tenants SET timezone=$1 WHERE id=app_current_tenant()`, *req.Timezone)
		}
		if req.Locale != nil {
			tx.Exec(ctx, `UPDATE tenants SET locale=$1 WHERE id=app_current_tenant()`, *req.Locale)
		}
		if req.Settings != nil {
			b, _ := json.Marshal(req.Settings)
			if _, err := tx.Exec(ctx, `UPDATE tenants SET settings = settings || $1::jsonb WHERE id=app_current_tenant()`, b); err != nil {
				return err
			}
		}
		return database.Audit(ctx, tx, "organization", "", "update", nil, req)
	})
}

type branchDTO struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Address   *string        `json:"address"`
	Phone     *string        `json:"phone"`
	Timezone  string         `json:"timezone"`
	WorkHours map[string]any `json:"work_hours"`
	IsActive  bool           `json:"is_active"`
	Rooms     int            `json:"classrooms"`
	Students  int            `json:"students"`
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) error {
	p := auth.FromContext(r.Context())
	out := []branchDTO{}
	err := h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `SELECT b.id, b.name, b.address, b.phone, b.timezone, b.work_hours, b.is_active,
			(SELECT count(*) FROM classrooms c WHERE c.branch_id=b.id), (SELECT count(*) FROM students s WHERE s.branch_id=b.id AND s.status='active')
			FROM branches b ORDER BY b.created_at`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var b branchDTO
			rows.Scan(&b.ID, &b.Name, &b.Address, &b.Phone, &b.Timezone, &b.WorkHours, &b.IsActive, &b.Rooms, &b.Students)
			if p.CanBranch(b.ID) {
				out = append(out, b)
			}
		}
		return rows.Err()
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, out)
}

type branchReq struct {
	Name      *string        `json:"name"`
	Address   *string        `json:"address"`
	Phone     *string        `json:"phone"`
	Timezone  *string        `json:"timezone"`
	WorkHours map[string]any `json:"work_hours"`
	IsActive  *bool          `json:"is_active"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) error {
	var req branchReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.Name == nil || strings.TrimSpace(*req.Name) == "" {
		return (&httpx.Validation{Fields: []httpx.FieldError{{Field: "name", Code: "REQUIRED"}}}).Err()
	}
	tz := "Asia/Almaty"
	if req.Timezone != nil {
		if _, err := time.LoadLocation(*req.Timezone); err != nil {
			return httpx.BadRequest("INVALID_TIMEZONE", "Неизвестный часовой пояс")
		}
		tz = *req.Timezone
	}
	var id string
	err := h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		var max *int
		var count int
		tx.QueryRow(r.Context(), `SELECT sp.max_branches, (SELECT count(*) FROM branches) FROM tenants t JOIN subscription_plans sp ON sp.code=t.plan_code WHERE t.id = app_current_tenant()`).Scan(&max, &count)
		if max != nil && count >= *max {
			return httpx.Conflict("PLAN_LIMIT", "Достигнут лимит филиалов на тарифе")
		}
		if err := tx.QueryRow(r.Context(), `INSERT INTO branches (tenant_id, name, address, phone, timezone) VALUES (app_current_tenant(), $1, $2, $3, $4) RETURNING id`,
			strings.TrimSpace(*req.Name), req.Address, req.Phone, tz).Scan(&id); err != nil {
			return err
		}
		return database.Audit(r.Context(), tx, "branch", id, "create", nil, req)
	})
	if err != nil {
		return err
	}
	return httpx.Created(w, map[string]string{"id": id})
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) error {
	var req branchReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	id := chi.URLParam(r, "id")
	return h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		ctx := r.Context()
		if req.Name != nil {
			tx.Exec(ctx, `UPDATE branches SET name=$2 WHERE id=$1`, id, *req.Name)
		}
		if req.Address != nil {
			tx.Exec(ctx, `UPDATE branches SET address=$2 WHERE id=$1`, id, *req.Address)
		}
		if req.Phone != nil {
			tx.Exec(ctx, `UPDATE branches SET phone=$2 WHERE id=$1`, id, *req.Phone)
		}
		if req.Timezone != nil {
			if _, err := time.LoadLocation(*req.Timezone); err != nil {
				return httpx.BadRequest("INVALID_TIMEZONE", "Неизвестный часовой пояс")
			}
			tx.Exec(ctx, `UPDATE branches SET timezone=$2 WHERE id=$1`, id, *req.Timezone)
		}
		if req.WorkHours != nil {
			b, _ := json.Marshal(req.WorkHours)
			tx.Exec(ctx, `UPDATE branches SET work_hours=$2 WHERE id=$1`, id, b)
		}
		if req.IsActive != nil {
			tx.Exec(ctx, `UPDATE branches SET is_active=$2 WHERE id=$1`, id, *req.IsActive)
		}
		return database.Audit(ctx, tx, "branch", id, "update", nil, req)
	})
}

type roomDTO struct {
	ID        string  `json:"id"`
	BranchID  string  `json:"branch_id"`
	Name      string  `json:"name"`
	Capacity  *int    `json:"capacity"`
	IsOnline  bool    `json:"is_online"`
	Equipment *string `json:"equipment"`
	Status    string  `json:"status"`
	SortOrder int     `json:"sort_order"`
}

func (h *Handler) classrooms(w http.ResponseWriter, r *http.Request) error {
	branch := r.URL.Query().Get("branch_id")
	out := []roomDTO{}
	err := h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `SELECT id, branch_id, name, capacity, is_online, equipment, status, sort_order FROM classrooms
			WHERE ($1 = '' OR branch_id::text=$1) ORDER BY is_online, sort_order, name`, branch)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c roomDTO
			rows.Scan(&c.ID, &c.BranchID, &c.Name, &c.Capacity, &c.IsOnline, &c.Equipment, &c.Status, &c.SortOrder)
			out = append(out, c)
		}
		return rows.Err()
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, out)
}

func (h *Handler) createRoom(w http.ResponseWriter, r *http.Request) error {
	var req roomDTO
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if strings.TrimSpace(req.Name) == "" || req.BranchID == "" {
		return httpx.BadRequest("VALIDATION_FAILED", "Укажите филиал и название кабинета")
	}
	if req.IsOnline {
		req.Capacity = nil // онлайн — без проверки вместимости
	}
	var id string
	err := h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(), `INSERT INTO classrooms (tenant_id, branch_id, name, capacity, is_online, equipment, sort_order)
			VALUES (app_current_tenant(), $1, $2, $3, $4, $5, (SELECT coalesce(max(sort_order),0)+1 FROM classrooms WHERE branch_id=$1)) RETURNING id`,
			req.BranchID, strings.TrimSpace(req.Name), req.Capacity, req.IsOnline, req.Equipment).Scan(&id)
	})
	if err != nil {
		return err
	}
	return httpx.Created(w, map[string]string{"id": id})
}

func (h *Handler) updateRoom(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Name      *string `json:"name"`
		Capacity  *int    `json:"capacity"`
		Equipment *string `json:"equipment"`
		Status    *string `json:"status"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	id := chi.URLParam(r, "id")
	return h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		ctx := r.Context()
		if req.Name != nil {
			tx.Exec(ctx, `UPDATE classrooms SET name=$2 WHERE id=$1`, id, *req.Name)
		}
		if req.Capacity != nil {
			tx.Exec(ctx, `UPDATE classrooms SET capacity=$2 WHERE id=$1 AND NOT is_online`, id, *req.Capacity)
		}
		if req.Equipment != nil {
			tx.Exec(ctx, `UPDATE classrooms SET equipment=$2 WHERE id=$1`, id, *req.Equipment)
		}
		if req.Status != nil {
			tx.Exec(ctx, `UPDATE classrooms SET status=$2 WHERE id=$1`, id, *req.Status)
		}
		return nil
	})
}

func (h *Handler) holidays(w http.ResponseWriter, r *http.Request) error {
	year := r.URL.Query().Get("year")
	if year == "" {
		year = time.Now().Format("2006")
	}
	out := []map[string]any{}
	err := h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `SELECT id, day, name, is_workday, tenant_id IS NULL FROM calendar_holidays
			WHERE extract(year FROM day)::text = $1 ORDER BY day`, year)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id, name string
			var day time.Time
			var workday, global bool
			rows.Scan(&id, &day, &name, &workday, &global)
			out = append(out, map[string]any{"id": id, "day": day.Format("2006-01-02"), "name": name, "is_workday": workday, "global": global})
		}
		return rows.Err()
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, out)
}

func (h *Handler) addHoliday(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Day      string `json:"day"`
		DayTo    string `json:"day_to"`
		Name     string `json:"name"`
		BranchID string `json:"branch_id"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	from, err := time.Parse("2006-01-02", req.Day)
	if err != nil || strings.TrimSpace(req.Name) == "" {
		return httpx.BadRequest("VALIDATION_FAILED", "Укажите дату и название")
	}
	to := from
	if req.DayTo != "" {
		if to, err = time.Parse("2006-01-02", req.DayTo); err != nil || to.Before(from) || to.Sub(from) > 62*24*time.Hour {
			return httpx.BadRequest("VALIDATION_FAILED", "Неверный период")
		}
	}
	return h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
			if _, err := tx.Exec(r.Context(), `INSERT INTO calendar_holidays (tenant_id, branch_id, day, name) VALUES (app_current_tenant(), NULLIF($1,'')::uuid, $2, $3)`,
				req.BranchID, d, strings.TrimSpace(req.Name)); err != nil {
				return err
			}
		}
		// уже созданные занятия в эти дни переводятся в «Требует решения»
		_, err := tx.Exec(r.Context(), `UPDATE lessons l SET status='needs_decision' FROM branches b
			WHERE b.id=l.branch_id AND l.status='planned' AND (l.starts_at AT TIME ZONE b.timezone)::date BETWEEN $1 AND $2
			AND ($3 = '' OR l.branch_id::text=$3)`, from, to, req.BranchID)
		return err
	})
}

func (h *Handler) deleteHoliday(w http.ResponseWriter, r *http.Request) error {
	return h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		_, err := tx.Exec(r.Context(), `DELETE FROM calendar_holidays WHERE id=$1 AND tenant_id IS NOT NULL`, chi.URLParam(r, "id"))
		return err
	})
}

func (h *Handler) rules(w http.ResponseWriter, r *http.Request) error {
	out := []map[string]any{}
	err := h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `SELECT status, charge, pay_teacher FROM attendance_charge_rules ORDER BY array_position(ARRAY['present','late','absent','excused','trial'], status)`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var s string
			var c, p bool
			rows.Scan(&s, &c, &p)
			out = append(out, map[string]any{"status": s, "charge": c, "pay_teacher": p})
		}
		return rows.Err()
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, out)
}

func (h *Handler) saveRules(w http.ResponseWriter, r *http.Request) error {
	var req []struct {
		Status     string `json:"status"`
		Charge     bool   `json:"charge"`
		PayTeacher bool   `json:"pay_teacher"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	return h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		for _, x := range req {
			if _, err := tx.Exec(r.Context(), `INSERT INTO attendance_charge_rules (tenant_id, status, charge, pay_teacher) VALUES (app_current_tenant(), $1, $2, $3)
				ON CONFLICT (tenant_id, status) DO UPDATE SET charge=EXCLUDED.charge, pay_teacher=EXCLUDED.pay_teacher`, x.Status, x.Charge, x.PayTeacher); err != nil {
				return err
			}
		}
		return database.Audit(r.Context(), tx, "attendance_rules", "", "update", nil, req)
	})
}
