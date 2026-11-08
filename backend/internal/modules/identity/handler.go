package identity

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"emp/internal/modules/organization"
	"emp/internal/platform/auth"
	"emp/internal/platform/database"
	"emp/internal/platform/httpx"
	"emp/internal/platform/middleware"
	"emp/internal/platform/validate"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

type Handler struct {
	DB           *database.DB
	Secret       string
	CookieSecure bool
}

const refreshCookie = "emp_refresh"

func (h *Handler) PublicRoutes(r chi.Router) {
	r.With(middleware.RateLimit(20, time.Minute)).Post("/auth/login", httpx.Handler(h.login).ServeHTTP)
	r.With(middleware.RateLimit(10, time.Minute)).Post("/auth/register", httpx.Handler(h.register).ServeHTTP)
	r.Post("/auth/refresh", httpx.Handler(h.refresh).ServeHTTP)
	r.Post("/auth/logout", httpx.Handler(h.logout).ServeHTTP)
}

func (h *Handler) PrivateRoutes(r chi.Router) {
	r.Get("/me", httpx.Handler(h.me).ServeHTTP)
	r.Post("/auth/logout-all", httpx.Handler(h.logoutAll).ServeHTTP)
}

// ---------------------------------------------------------------- регистрация

type registerReq struct {
	FullName   string `json:"full_name"`
	Phone      string `json:"phone"`
	Email      string `json:"email"`
	Password   string `json:"password"`
	Accept     bool   `json:"accept_terms"`
	CenterName string `json:"center_name"`
	CenterType string `json:"center_type"` // language, ent, kids, it, other
	City       string `json:"city"`
	Size       string `json:"size"`
	BranchName string `json:"branch_name"`
	Address    string `json:"address"`
	Template   string `json:"template"`
	Locale     string `json:"locale"`
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) error {
	var req registerReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	v := &httpx.Validation{}
	req.Email = strings.ToLower(httpx.Trim(req.Email))
	phone, ok := validate.Phone(req.Phone)
	if httpx.Trim(req.FullName) == "" {
		v.Add("full_name", "REQUIRED")
	}
	if !ok || phone == "" {
		v.Add("phone", "INVALID_FORMAT")
	}
	if req.Email == "" || !validate.Email(req.Email) {
		v.Add("email", "INVALID_FORMAT")
	}
	if len([]rune(req.Password)) < 10 {
		v.Add("password", "TOO_SHORT")
	}
	if !req.Accept {
		v.Add("accept_terms", "REQUIRED")
	}
	if httpx.Trim(req.CenterName) == "" {
		v.Add("center_name", "REQUIRED")
	}
	if err := v.Err(); err != nil {
		return err
	}
	if req.Locale == "" {
		req.Locale = "ru"
	}
	if req.BranchName == "" {
		req.BranchName = "Основной филиал"
		if req.City != "" {
			req.BranchName = req.City
		}
	}
	if req.Template == "" {
		req.Template = req.CenterType
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		return err
	}
	ctx := r.Context()
	var userID, tenantID string
	// Создание организации — системная операция: tenant ещё не существует, поэтому пул Sys.
	err = pgx.BeginFunc(ctx, h.DB.Sys, func(tx pgx.Tx) error {
		var exists bool
		tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE lower(email)=$1)`, req.Email).Scan(&exists)
		if exists {
			return httpx.Conflict("EMAIL_TAKEN", "Пользователь с таким email уже зарегистрирован")
		}
		if err := tx.QueryRow(ctx, `INSERT INTO tenants (name, center_type, city, locale, plan_code, status, trial_ends_at, settings)
			VALUES ($1,$2,$3,$4,'business','trial',NOW() + interval '14 days', $5) RETURNING id`,
			httpx.Trim(req.CenterName), orDefault(req.CenterType, "other"), httpx.Ptr(req.City), req.Locale,
			organization.DefaultSettings(req.Size)).Scan(&tenantID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO branches (tenant_id, name, address) VALUES ($1,$2,$3)`, tenantID, req.BranchName, httpx.Ptr(req.Address)); err != nil {
			return err
		}
		ownerRole, err := organization.Bootstrap(ctx, tx, tenantID, req.Template)
		if err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `INSERT INTO users (tenant_id, email, phone, full_name, password_hash, locale) VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
			tenantID, req.Email, phone, httpx.Trim(req.FullName), hash, req.Locale).Scan(&userID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO user_roles (tenant_id, user_id, role_id) VALUES ($1,$2,$3)`, tenantID, userID, ownerRole); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO audit_logs (tenant_id, user_id, entity_type, entity_id, action, new_values) VALUES ($1,$2,'tenant',$1,'registered', $3)`,
			tenantID, userID, map[string]any{"name": req.CenterName, "template": req.Template})
		return err
	})
	if err != nil {
		return err
	}
	return h.issue(w, r, userID, tenantID, false)
}

// ---------------------------------------------------------------- вход

func (h *Handler) login(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	ctx := r.Context()
	var id, hash, status string
	var tenantID *string
	var platform bool
	var lockedUntil *time.Time
	err := h.DB.Sys.QueryRow(ctx, `SELECT id, tenant_id::text, password_hash, is_platform_admin, status, locked_until FROM users WHERE lower(email)=lower($1)`,
		httpx.Trim(req.Email)).Scan(&id, &tenantID, &hash, &platform, &status, &lockedUntil)
	invalid := httpx.NewError(401, "INVALID_CREDENTIALS", "Неверный email или пароль")
	if errors.Is(err, pgx.ErrNoRows) {
		auth.CheckPassword("$argon2id$v=19$m=65536,t=2,p=2$c2FsdHNhbHRzYWx0$aGFzaGhhc2hoYXNoaGFzaGhhc2hoYXNoaGFzaGhh", req.Password) // выравнивание времени ответа
		return invalid
	}
	if err != nil {
		return err
	}
	if lockedUntil != nil && lockedUntil.After(time.Now()) {
		return httpx.NewError(429, "ACCOUNT_LOCKED", "Слишком много неудачных попыток. Попробуйте через 15 минут")
	}
	if !auth.CheckPassword(hash, req.Password) {
		h.DB.Sys.Exec(ctx, `UPDATE users SET failed_logins = failed_logins + 1,
			locked_until = CASE WHEN failed_logins + 1 >= 5 THEN NOW() + interval '15 minutes' END WHERE id=$1`, id)
		return invalid
	}
	if status != "active" {
		return httpx.NewError(403, "USER_BLOCKED", "Пользователь заблокирован")
	}
	if tenantID != nil {
		var tStatus string
		h.DB.Sys.QueryRow(ctx, `SELECT status FROM tenants WHERE id=$1`, *tenantID).Scan(&tStatus)
		if tStatus == "suspended" {
			return httpx.NewError(403, "TENANT_SUSPENDED", "Доступ организации приостановлен. Свяжитесь с поддержкой")
		}
	}
	h.DB.Sys.Exec(ctx, `UPDATE users SET failed_logins=0, locked_until=NULL, last_login_at=NOW() WHERE id=$1`, id)
	tid := ""
	if tenantID != nil {
		tid = *tenantID
		h.DB.Sys.Exec(ctx, `INSERT INTO audit_logs (tenant_id, user_id, entity_type, entity_id, action, ip_address, user_agent) VALUES ($1,$2,'user',$2,'login',$3,$4)`,
			tid, id, middleware.ClientIP(r), r.UserAgent())
	}
	return h.issue(w, r, id, tid, platform)
}

// IssueSession выдаёт сессию сотрудника (используется входом через Mini App).
func (h *Handler) IssueSession(w http.ResponseWriter, r *http.Request, userID, tenantID string, extra map[string]any) error {
	return h.issue(w, r, userID, tenantID, false, extra)
}

// ---------------------------------------------------------------- токены

func (h *Handler) issue(w http.ResponseWriter, r *http.Request, userID, tenantID string, platform bool, extra ...map[string]any) error {
	access, err := auth.IssueAccess(h.Secret, userID, tenantID, platform)
	if err != nil {
		return err
	}
	refresh, hash := auth.NewRefreshToken()
	if _, err := h.DB.Sys.Exec(r.Context(), `INSERT INTO sessions (user_id, refresh_hash, user_agent, ip, expires_at) VALUES ($1,$2,$3,$4,$5)`,
		userID, hash, r.UserAgent(), middleware.ClientIP(r), time.Now().Add(auth.RefreshTTL)); err != nil {
		return err
	}
	h.setCookie(w, refresh, auth.RefreshTTL)
	out := map[string]any{"access_token": access, "expires_in": int(auth.AccessTTL.Seconds())}
	for _, e := range extra {
		for k, v := range e {
			out[k] = v
		}
	}
	return httpx.OK(w, out)
}

func (h *Handler) setCookie(w http.ResponseWriter, value string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{Name: refreshCookie, Value: value, Path: "/api/v1/auth", HttpOnly: true,
		Secure: h.CookieSecure, SameSite: http.SameSiteStrictMode, MaxAge: int(ttl.Seconds())})
}

// refresh ротирует refresh-токен: старый отзывается, выдаётся новый.
// Повторное использование отозванного токена завершает все сессии пользователя (защита от кражи).
func (h *Handler) refresh(w http.ResponseWriter, r *http.Request) error {
	c, err := r.Cookie(refreshCookie)
	if err != nil {
		return httpx.Unauthorized()
	}
	ctx := r.Context()
	var sid, userID string
	var revoked *time.Time
	var expires time.Time
	err = h.DB.Sys.QueryRow(ctx, `SELECT id, user_id, revoked_at, expires_at FROM sessions WHERE refresh_hash=$1`, auth.HashRefresh(c.Value)).
		Scan(&sid, &userID, &revoked, &expires)
	if err != nil {
		return httpx.Unauthorized()
	}
	if revoked != nil {
		h.DB.Sys.Exec(ctx, `UPDATE sessions SET revoked_at=NOW() WHERE user_id=$1 AND revoked_at IS NULL`, userID)
		return httpx.Unauthorized()
	}
	if expires.Before(time.Now()) {
		return httpx.Unauthorized()
	}
	var tenantID *string
	var platform bool
	var status string
	if err := h.DB.Sys.QueryRow(ctx, `SELECT tenant_id::text, is_platform_admin, status FROM users WHERE id=$1`, userID).Scan(&tenantID, &platform, &status); err != nil || status != "active" {
		return httpx.Unauthorized()
	}
	tag, _ := h.DB.Sys.Exec(ctx, `UPDATE sessions SET revoked_at=NOW() WHERE id=$1 AND revoked_at IS NULL`, sid)
	if tag.RowsAffected() == 0 {
		return httpx.Unauthorized()
	}
	tid := ""
	if tenantID != nil {
		tid = *tenantID
	}
	return h.issue(w, r, userID, tid, platform)
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) error {
	if c, err := r.Cookie(refreshCookie); err == nil {
		h.DB.Sys.Exec(r.Context(), `UPDATE sessions SET revoked_at=NOW() WHERE refresh_hash=$1`, auth.HashRefresh(c.Value))
	}
	h.setCookie(w, "", -time.Second)
	return httpx.NoContent(w)
}

func (h *Handler) logoutAll(w http.ResponseWriter, r *http.Request) error {
	p := auth.FromContext(r.Context())
	h.DB.Sys.Exec(r.Context(), `UPDATE sessions SET revoked_at=NOW() WHERE user_id=$1 AND revoked_at IS NULL`, p.UserID)
	h.setCookie(w, "", -time.Second)
	return httpx.NoContent(w)
}

// ---------------------------------------------------------------- профиль

func (h *Handler) me(w http.ResponseWriter, r *http.Request) error {
	p := auth.FromContext(r.Context())
	ctx := r.Context()
	if p.TenantID == "" {
		var name, email string
		h.DB.Sys.QueryRow(ctx, `SELECT full_name, email FROM users WHERE id=$1`, p.UserID).Scan(&name, &email)
		return httpx.OK(w, map[string]any{"user": map[string]any{"id": p.UserID, "full_name": name, "email": email}, "is_platform_admin": true})
	}
	out := map[string]any{}
	err := h.DB.Tx(ctx, func(tx pgx.Tx) error {
		var email, locale string
		var tName, plan, tStatus, tz, centerType string
		var trialEnds *time.Time
		var paidUntil *time.Time
		if err := tx.QueryRow(ctx, `SELECT u.email, u.locale, t.name, t.plan_code, t.status, t.timezone, t.trial_ends_at, t.paid_until, t.center_type
			FROM users u JOIN tenants t ON t.id=u.tenant_id WHERE u.id=$1`, p.UserID).
			Scan(&email, &locale, &tName, &plan, &tStatus, &tz, &trialEnds, &paidUntil, &centerType); err != nil {
			return err
		}
		perms := []string{}
		for k := range p.Permissions {
			perms = append(perms, k)
		}
		sort.Strings(perms)
		rows, err := tx.Query(ctx, `SELECT id, name, address, timezone FROM branches WHERE is_active ORDER BY created_at`)
		if err != nil {
			return err
		}
		type br struct {
			ID       string  `json:"id"`
			Name     string  `json:"name"`
			Address  *string `json:"address"`
			Timezone string  `json:"timezone"`
		}
		branches := []br{}
		for rows.Next() {
			var b br
			rows.Scan(&b.ID, &b.Name, &b.Address, &b.Timezone)
			if p.CanBranch(b.ID) {
				branches = append(branches, b)
			}
		}
		rows.Close()
		out["user"] = map[string]any{"id": p.UserID, "full_name": p.FullName, "email": email, "locale": locale}
		out["tenant"] = map[string]any{"id": p.TenantID, "name": tName, "plan": plan, "status": tStatus, "timezone": tz,
			"trial_ends_at": trialEnds, "paid_until": paidUntil, "center_type": centerType}
		out["permissions"] = perms
		out["roles"] = p.RoleCodes
		out["branches"] = branches
		out["employee_id"] = p.EmployeeID
		out["all_branches"] = p.AllBranches
		return nil
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, out)
}

func orDefault(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}

var _ = context.Background
