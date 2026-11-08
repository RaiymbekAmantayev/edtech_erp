// Пакет portal — личный кабинет (Telegram Mini App) учеников, родителей и преподавателей и Telegram-бот школы.
//
// Ученик и родитель входят по своему ИИН из карточки в ERP; первый вход — с паролем по умолчанию 12345,
// который можно сменить в профиле. Преподаватель входит только через Telegram по приглашению администратора
// и работает обычной сессией сотрудника с ролью «Преподаватель».
package portal

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"emp/internal/modules/identity"
	"emp/internal/platform/auth"
	"emp/internal/platform/database"
	"emp/internal/platform/httpx"
	mw "emp/internal/platform/middleware"
	"emp/internal/platform/telegram"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

const DefaultPassword = "12345"

type Handler struct {
	DB        *database.DB
	Secret    string
	Platform  telegram.Platform
	PublicURL string
	Staff     *identity.Handler
}

func (h *Handler) PublicRoutes(r chi.Router) {
	r.Get("/miniapp/school", httpx.Handler(h.school).ServeHTTP)
	r.With(mw.RateLimit(30, time.Minute)).Post("/miniapp/auth", httpx.Handler(h.miniAppAuth).ServeHTTP)
	r.With(mw.RateLimit(20, time.Minute)).Post("/portal/login", httpx.Handler(h.login).ServeHTTP)
	r.Group(func(r chi.Router) {
		r.Use(h.requireAccount)
		r.Get("/portal/me", httpx.Handler(h.me).ServeHTTP)
		r.Post("/portal/password", httpx.Handler(h.changePassword).ServeHTTP)
		r.Get("/portal/students/{id}", httpx.Handler(h.studentOverview).ServeHTTP)
	})
}

// ---------------------------------------------------------------- школа

func validUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
			return false
		}
	}
	return true
}

// activeTenant — организация существует и не приостановлена.
func (h *Handler) activeTenant(ctx context.Context, id string) (string, error) {
	if !validUUID(id) {
		return "", httpx.NotFound("SCHOOL_NOT_FOUND")
	}
	var name, status string
	if err := h.DB.Sys.QueryRow(ctx, `SELECT name, status FROM tenants WHERE id=$1`, id).Scan(&name, &status); err != nil {
		return "", httpx.NotFound("SCHOOL_NOT_FOUND")
	}
	if status == "suspended" {
		return "", httpx.NewError(403, "TENANT_SUSPENDED", "Доступ учебного центра приостановлен")
	}
	return name, nil
}

func (h *Handler) school(w http.ResponseWriter, r *http.Request) error {
	id := r.URL.Query().Get("t")
	name, err := h.activeTenant(r.Context(), id)
	if err != nil {
		return err
	}
	out := map[string]any{"id": id, "name": name}
	if bot, ok := h.Platform.For(r.Context(), h.DB.Sys, id); ok {
		out["bot_username"] = bot.Username
	}
	return httpx.OK(w, out)
}

// ---------------------------------------------------------------- вход из Telegram

// miniAppAuth — автоматический вход при открытии Mini App: подпись initData проверяется токеном бота школы.
// Сотрудник с привязанным Telegram получает сессию сотрудника, ученик/родитель — токен кабинета.
func (h *Handler) miniAppAuth(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		TenantID string `json:"tenant_id"`
		InitData string `json:"init_data"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	ctx := r.Context()
	if _, err := h.activeTenant(ctx, req.TenantID); err != nil {
		return err
	}
	tgID, _, err := h.verify(ctx, req.TenantID, req.InitData)
	if err != nil {
		return err
	}
	var userID string
	err = h.DB.Sys.QueryRow(ctx, `SELECT id FROM users WHERE tenant_id=$1 AND telegram_id=$2 AND status='active'`, req.TenantID, tgID).Scan(&userID)
	if err == nil {
		h.DB.Sys.Exec(ctx, `UPDATE users SET last_login_at=NOW() WHERE id=$1`, userID)
		return h.Staff.IssueSession(w, r, userID, req.TenantID, map[string]any{"kind": "staff"})
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	var accID, kind string
	err = h.DB.Sys.QueryRow(ctx, `SELECT id, kind FROM portal_accounts WHERE tenant_id=$1 AND telegram_id=$2 AND status='active'
		ORDER BY last_login_at DESC NULLS LAST LIMIT 1`, req.TenantID, tgID).Scan(&accID, &kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.OK(w, map[string]any{"linked": false})
	}
	if err != nil {
		return err
	}
	h.DB.Sys.Exec(ctx, `UPDATE portal_accounts SET last_login_at=NOW() WHERE id=$1`, accID)
	return h.issuePortal(w, accID, req.TenantID, kind, false)
}

// verify проверяет initData; пустая строка — вход из обычного браузера (tgID = 0).
func (h *Handler) verify(ctx context.Context, tenantID, initData string) (int64, telegram.Bot, error) {
	if initData == "" {
		return 0, telegram.Bot{}, httpx.BadRequest("TELEGRAM_REQUIRED", "Откройте кабинет из Telegram")
	}
	bot, ok := h.Platform.For(ctx, h.DB.Sys, tenantID)
	if !ok {
		return 0, bot, httpx.BadRequest("TELEGRAM_DISABLED", "Telegram-бот учебного центра не подключён")
	}
	tgID, err := telegram.VerifyInitData(initData, bot.Token, 24*time.Hour)
	if err != nil {
		return 0, bot, httpx.NewError(401, "TELEGRAM_INVALID", "Подпись Telegram не прошла проверку. Откройте кабинет заново из бота")
	}
	return tgID, bot, nil
}

func (h *Handler) issuePortal(w http.ResponseWriter, accID, tenantID, kind string, defaultPassword bool) error {
	token, err := auth.IssuePortal(h.Secret, accID, tenantID, kind)
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{"portal_token": token, "kind": kind, "expires_in": int(auth.PortalTTL.Seconds()), "default_password": defaultPassword})
}

// ---------------------------------------------------------------- вход по ИИН

// login — вход ученика/родителя по ИИН. Если учётной записи ещё нет, она создаётся при первом входе
// с паролем 12345 — при условии, что ИИН есть в карточке ученика/родителя в ERP.
func (h *Handler) login(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		TenantID string `json:"tenant_id"`
		Kind     string `json:"kind"`
		IIN      string `json:"iin"`
		Password string `json:"password"`
		InitData string `json:"init_data"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	ctx := r.Context()
	if _, err := h.activeTenant(ctx, req.TenantID); err != nil {
		return err
	}
	iin := strings.TrimSpace(req.IIN)
	v := &httpx.Validation{}
	if req.Kind != "student" && req.Kind != "parent" {
		v.Add("kind", "INVALID_OPTION")
	}
	if len(iin) != 12 || strings.Trim(iin, "0123456789") != "" {
		v.Add("iin", "INVALID_IIN")
	}
	if req.Password == "" {
		v.Add("password", "REQUIRED")
	}
	if err := v.Err(); err != nil {
		return err
	}
	// Telegram необязателен: кабинет открывается и в браузере
	var tgID int64
	var bot telegram.Bot
	if req.InitData != "" {
		var err error
		if tgID, bot, err = h.verify(ctx, req.TenantID, req.InitData); err != nil {
			return err
		}
	}
	invalid := httpx.NewError(401, "INVALID_CREDENTIALS", "Неверный ИИН или пароль")
	var accID string
	err := h.DB.InTenant(ctx, req.TenantID, func(tx pgx.Tx) error {
		var hash, status string
		var lockedUntil *time.Time
		err := tx.QueryRow(ctx, `SELECT id, password_hash, status, locked_until FROM portal_accounts WHERE kind=$1 AND iin=$2`, req.Kind, iin).
			Scan(&accID, &hash, &status, &lockedUntil)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			// первый вход: «регистрация» по ИИН из карточки
			personID, err := findPerson(ctx, tx, req.Kind, iin)
			if err != nil {
				return err
			}
			if req.Password != DefaultPassword {
				return httpx.NewError(401, "INVALID_CREDENTIALS", "Для первого входа используйте пароль 12345")
			}
			hash, err := auth.HashPassword(DefaultPassword)
			if err != nil {
				return err
			}
			col := map[string]string{"student": "student_id", "parent": "parent_id"}[req.Kind]
			if err := tx.QueryRow(ctx, `INSERT INTO portal_accounts (tenant_id, kind, `+col+`, iin, password_hash) VALUES (app_current_tenant(), $1, $2, $3, $4) RETURNING id`,
				req.Kind, personID, iin, hash).Scan(&accID); err != nil {
				return err
			}
			if err := database.Audit(ctx, tx, "portal_account", accID, "registered", nil, map[string]any{"kind": req.Kind}); err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			if lockedUntil != nil && lockedUntil.After(time.Now()) {
				return httpx.NewError(429, "ACCOUNT_LOCKED", "Слишком много неудачных попыток. Попробуйте через 15 минут")
			}
			if !auth.CheckPassword(hash, req.Password) {
				// отдельной транзакцией: текущая будет откатана ошибкой
				h.DB.InTenant(ctx, req.TenantID, func(tx pgx.Tx) error {
					_, err := tx.Exec(ctx, `UPDATE portal_accounts SET failed_logins = failed_logins + 1,
						locked_until = CASE WHEN failed_logins + 1 >= 5 THEN NOW() + interval '15 minutes' END WHERE id=$1`, accID)
					return err
				})
				return invalid
			}
			if status != "active" {
				return httpx.NewError(403, "ACCOUNT_BLOCKED", "Доступ к кабинету закрыт. Обратитесь к администратору центра")
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE portal_accounts SET failed_logins=0, locked_until=NULL, last_login_at=NOW() WHERE id=$1`, accID); err != nil {
			return err
		}
		if tgID != 0 {
			return linkTelegram(ctx, tx, accID, req.Kind, tgID, bot.ID)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return h.issuePortal(w, accID, req.TenantID, req.Kind, req.Password == DefaultPassword)
}

func findPerson(ctx context.Context, tx pgx.Tx, kind, iin string) (string, error) {
	q := `SELECT id FROM students WHERE iin=$1 AND archived_at IS NULL ORDER BY created_at DESC LIMIT 1`
	if kind == "parent" {
		q = `SELECT id FROM parents WHERE iin=$1 AND archived_at IS NULL ORDER BY created_at DESC LIMIT 1`
	}
	var id string
	err := tx.QueryRow(ctx, q, iin).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		who := map[string]string{"student": "ученика", "parent": "родителя"}[kind]
		return "", httpx.NewError(404, "IIN_NOT_FOUND", "ИИН не найден в карточках "+who+" учебного центра. Обратитесь к администратору")
	}
	return id, err
}

// linkTelegram привязывает Telegram к учётной записи; у родителя он же становится каналом уведомлений.
func linkTelegram(ctx context.Context, tx pgx.Tx, accID, kind string, tgID, botID int64) error {
	if _, err := tx.Exec(ctx, `UPDATE portal_accounts SET telegram_id = NULL WHERE telegram_id=$1 AND id<>$2`, tgID, accID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE portal_accounts SET telegram_id=$1 WHERE id=$2`, tgID, accID); err != nil {
		return err
	}
	if kind == "parent" {
		if _, err := tx.Exec(ctx, `UPDATE parents SET telegram_chat_id=$1, telegram_bot_id=$2, preferred_channel='telegram'
			WHERE id = (SELECT parent_id FROM portal_accounts WHERE id=$3)`, tgID, botID, accID); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------- сессия кабинета

type Account struct {
	ID, TenantID, Kind string
	StudentID          *string
	ParentID           *string
}

type ctxKey struct{}

func accountFrom(ctx context.Context) *Account { a, _ := ctx.Value(ctxKey{}).(*Account); return a }

func (h *Handler) requireAccount(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok {
			httpx.WriteError(w, r, httpx.Unauthorized())
			return
		}
		c, err := auth.ParsePortal(h.Secret, token)
		if err != nil {
			httpx.WriteError(w, r, httpx.NewError(401, "TOKEN_EXPIRED", "Сессия истекла"))
			return
		}
		a := &Account{ID: c.Subject, TenantID: c.TenantID, Kind: c.Kind}
		err = h.DB.InTenant(r.Context(), a.TenantID, func(tx pgx.Tx) error {
			var status string
			if err := tx.QueryRow(r.Context(), `SELECT student_id::text, parent_id::text, status FROM portal_accounts WHERE id=$1`, a.ID).
				Scan(&a.StudentID, &a.ParentID, &status); err != nil {
				return err
			}
			if status != "active" {
				return errors.New("blocked")
			}
			return nil
		})
		if err != nil {
			httpx.WriteError(w, r, httpx.Unauthorized())
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, a)))
	})
}

func (h *Handler) changePassword(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if len([]rune(req.New)) < 6 {
		return (&httpx.Validation{Fields: []httpx.FieldError{{Field: "new_password", Code: "TOO_SHORT"}}}).Err()
	}
	a := accountFrom(r.Context())
	ctx := r.Context()
	err := h.DB.InTenant(ctx, a.TenantID, func(tx pgx.Tx) error {
		var hash string
		if err := tx.QueryRow(ctx, `SELECT password_hash FROM portal_accounts WHERE id=$1`, a.ID).Scan(&hash); err != nil {
			return err
		}
		if !auth.CheckPassword(hash, req.Current) {
			return (&httpx.Validation{Fields: []httpx.FieldError{{Field: "current_password", Code: "INVALID"}}}).Err()
		}
		newHash, err := auth.HashPassword(req.New)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE portal_accounts SET password_hash=$1 WHERE id=$2`, newHash, a.ID); err != nil {
			return err
		}
		return database.Audit(ctx, tx, "portal_account", a.ID, "password_changed", nil, nil)
	})
	if err != nil {
		return err
	}
	return httpx.NoContent(w)
}
