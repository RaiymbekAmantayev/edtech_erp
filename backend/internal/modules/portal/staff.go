package portal

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"emp/internal/platform/auth"
	"emp/internal/platform/database"
	"emp/internal/platform/httpx"
	mw "emp/internal/platform/middleware"
	"emp/internal/platform/telegram"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

// StaffRoutes — для сотрудников организации: кабинет преподавателя, бот школы, приглашения.
func (h *Handler) StaffRoutes(r chi.Router) {
	r.Get("/teacher/cabinet", httpx.Handler(h.teacherCabinet).ServeHTTP)
	r.With(mw.Perm("settings.manage")).Get("/settings/telegram-bot", httpx.Handler(h.getBot).ServeHTTP)
	r.With(mw.Perm("settings.manage")).Put("/settings/telegram-bot", httpx.Handler(h.saveBot).ServeHTTP)
	r.With(mw.Perm("settings.manage")).Delete("/settings/telegram-bot", httpx.Handler(h.deleteBot).ServeHTTP)
	r.With(mw.Perm("employees.view")).Get("/employees/{id}/telegram", httpx.Handler(h.employeeTelegram).ServeHTTP)
	r.With(mw.Perm("employees.edit")).Post("/employees/{id}/telegram-invite", httpx.Handler(h.invite).ServeHTTP)
	r.With(mw.Perm("employees.edit")).Delete("/employees/{id}/telegram", httpx.Handler(h.revoke).ServeHTTP)
}

// ---------------------------------------------------------------- кабинет преподавателя

func (h *Handler) teacherCabinet(w http.ResponseWriter, r *http.Request) error {
	p := auth.FromContext(r.Context())
	if p.EmployeeID == "" {
		return httpx.NewError(403, "NOT_TEACHER", "Пользователь не связан с карточкой преподавателя")
	}
	ctx := r.Context()
	emp := p.EmployeeID
	out := map[string]any{}
	err := h.DB.Tx(ctx, func(tx pgx.Tx) error {
		var name, payType string
		var phone, email, position *string
		var subjects []string
		var hired *time.Time
		if err := tx.QueryRow(ctx, `SELECT full_name, phone, email, position, subjects, hired_at, pay_type FROM employees WHERE id=$1`, emp).
			Scan(&name, &phone, &email, &position, &subjects, &hired, &payType); err != nil {
			return err
		}
		var school string
		tx.QueryRow(ctx, `SELECT name FROM tenants WHERE id = app_current_tenant()`).Scan(&school)
		var branches []string
		rows, err := tx.Query(ctx, `SELECT b.name FROM employees e JOIN branches b ON b.id = ANY(e.branch_ids) WHERE e.id=$1 ORDER BY b.name`, emp)
		if err != nil {
			return err
		}
		branches, err = pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		out["profile"] = map[string]any{"id": emp, "full_name": name, "phone": phone, "email": email, "position": position, "subjects": subjects,
			"hired_at": hired, "branches": branches, "school": school}

		var monthDone, totalDone, unmarked, students, groups, marks, attended int
		tx.QueryRow(ctx, `SELECT
				count(*) FILTER (WHERE status='done' AND starts_at >= date_trunc('month', NOW())),
				count(*) FILTER (WHERE status='done'),
				count(*) FILTER (WHERE status='planned' AND starts_at < NOW())
			FROM lessons WHERE teacher_id=$1`, emp).Scan(&monthDone, &totalDone, &unmarked)
		tx.QueryRow(ctx, `SELECT count(DISTINCT g.id), count(DISTINCT gs.student_id) FROM groups g
			LEFT JOIN group_students gs ON gs.group_id=g.id AND gs.left_at IS NULL
			WHERE g.teacher_id=$1 AND g.status='active'`, emp).Scan(&groups, &students)
		tx.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE a.status IN ('present','late','trial'))
			FROM attendance a JOIN lessons l ON l.id=a.lesson_id WHERE l.teacher_id=$1 AND l.starts_at > NOW() - interval '30 days'`, emp).Scan(&marks, &attended)
		rate := 0
		if marks > 0 {
			rate = attended * 100 / marks
		}
		out["stats"] = map[string]any{"lessons_month": monthDone, "lessons_total": totalDone, "unmarked": unmarked,
			"groups": groups, "students": students, "attendance_rate_30d": rate}

		lessonQ := `SELECT l.id::text, l.starts_at, l.ends_at, l.status, g.name, NULL::text, cl.name, l.topic, l.meeting_url
			FROM lessons l JOIN groups g ON g.id=l.group_id LEFT JOIN classrooms cl ON cl.id=l.classroom_id WHERE l.teacher_id=$1 AND `
		if out["schedule"], err = collect(ctx, tx, lessonQ+`l.ends_at >= date_trunc('day', NOW()) AND l.starts_at < NOW() + interval '7 days' ORDER BY l.starts_at LIMIT 60`,
			[]any{emp}, scanLesson); err != nil {
			return err
		}
		if out["to_mark"], err = collect(ctx, tx, lessonQ+`l.status='planned' AND l.starts_at < NOW() AND l.starts_at > NOW() - interval '30 days' ORDER BY l.starts_at DESC LIMIT 30`,
			[]any{emp}, scanLesson); err != nil {
			return err
		}
		out["conducted"], err = collect(ctx, tx, `SELECT l.id::text, l.starts_at, g.name, l.topic,
				count(a.id), count(a.id) FILTER (WHERE a.status IN ('present','late','trial'))
			FROM lessons l JOIN groups g ON g.id=l.group_id LEFT JOIN attendance a ON a.lesson_id=l.id
			WHERE l.teacher_id=$1 AND l.status='done' GROUP BY l.id, g.name ORDER BY l.starts_at DESC LIMIT 30`,
			[]any{emp}, func(rows pgx.Rows) (map[string]any, error) {
				var id, group string
				var at time.Time
				var topic *string
				var total, present int
				err := rows.Scan(&id, &at, &group, &topic, &total, &present)
				return map[string]any{"id": id, "starts_at": at, "group": group, "topic": topic, "marked": total, "present": present}, err
			})
		return err
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, out)
}

// ---------------------------------------------------------------- бот школы

var notifyTypes = []string{"attendance", "lessons", "subscription", "payment"}

func (h *Handler) getBot(w http.ResponseWriter, r *http.Request) error {
	p := auth.FromContext(r.Context())
	ctx := r.Context()
	out := map[string]any{"connected": false, "miniapp_url": telegram.MiniAppURL(h.PublicURL, p.TenantID), "platform_bot": nil}
	if h.Platform.Token != "" {
		out["platform_bot"] = h.Platform.Username
	}
	err := h.DB.Tx(ctx, func(tx pgx.Tx) error {
		var username string
		var enabled bool
		var welcome, lastErr *string
		var notify map[string]bool
		var updated time.Time
		err := tx.QueryRow(ctx, `SELECT username, enabled, welcome_text, notify, last_error, updated_at FROM tenant_bots`).
			Scan(&username, &enabled, &welcome, &notify, &lastErr, &updated)
		if err == nil {
			out["connected"] = true
			out["username"] = username
			out["enabled"] = enabled
			out["welcome_text"] = welcome
			out["last_error"] = lastErr
			out["updated_at"] = updated
		} else if err != pgx.ErrNoRows {
			return err
		}
		n := map[string]bool{}
		for _, t := range notifyTypes {
			v, set := notify[t]
			n[t] = !set || v // по умолчанию включено
		}
		out["notify"] = n
		var parents, accounts, teachers int
		tx.QueryRow(ctx, `SELECT count(*) FROM parents WHERE telegram_chat_id IS NOT NULL AND archived_at IS NULL`).Scan(&parents)
		tx.QueryRow(ctx, `SELECT count(*) FROM portal_accounts WHERE status='active'`).Scan(&accounts)
		tx.QueryRow(ctx, `SELECT count(*) FROM users WHERE telegram_id IS NOT NULL AND status='active'`).Scan(&teachers)
		out["stats"] = map[string]int{"parents_notified": parents, "portal_accounts": accounts, "staff_linked": teachers}
		return nil
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, out)
}

// saveBot подключает бота по токену (getMe) и обновляет настройки; кнопка меню бота открывает кабинет школы.
func (h *Handler) saveBot(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Token       *string         `json:"token"`
		Enabled     *bool           `json:"enabled"`
		WelcomeText *string         `json:"welcome_text"`
		Notify      map[string]bool `json:"notify"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	p := auth.FromContext(r.Context())
	ctx := r.Context()
	if req.Token != nil {
		token := strings.TrimSpace(*req.Token)
		var me struct {
			ID       int64  `json:"id"`
			Username string `json:"username"`
		}
		if err := telegram.Call(ctx, token, "getMe", map[string]any{}, &me); err != nil || me.ID == 0 {
			return (&httpx.Validation{Fields: []httpx.FieldError{{Field: "token", Code: "INVALID"}}}).Err()
		}
		if me.ID == telegram.BotID(h.Platform.Token) {
			return httpx.Conflict("BOT_IS_PLATFORM", "Это общий бот платформы — создайте отдельного бота школы в @BotFather")
		}
		var other bool
		h.DB.Sys.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM tenant_bots WHERE bot_id=$1 AND tenant_id<>$2)`, me.ID, p.TenantID).Scan(&other)
		if other {
			return httpx.Conflict("BOT_TAKEN", "Этот бот уже подключён к другому учебному центру")
		}
		// бот работает через getUpdates — вебхук нужно снять; кнопка меню открывает кабинет
		var setupErr *string
		if err := telegram.Call(ctx, token, "deleteWebhook", map[string]any{}, nil); err != nil {
			s := err.Error()
			setupErr = &s
		}
		if err := telegram.Call(ctx, token, "setChatMenuButton", map[string]any{"menu_button": map[string]any{
			"type": "web_app", "text": "Кабинет", "web_app": map[string]string{"url": telegram.MiniAppURL(h.PublicURL, p.TenantID)}}}, nil); err != nil {
			s := "Кнопка «Кабинет» не установлена: " + err.Error()
			setupErr = &s
		}
		err := h.DB.Tx(ctx, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `INSERT INTO tenant_bots (tenant_id, token, bot_id, username, last_error) VALUES (app_current_tenant(), $1, $2, $3, $4)
				ON CONFLICT (tenant_id) DO UPDATE SET token=EXCLUDED.token, bot_id=EXCLUDED.bot_id, username=EXCLUDED.username,
					last_error=EXCLUDED.last_error, enabled=TRUE, updated_at=NOW()`, token, me.ID, me.Username, setupErr)
			if err != nil {
				return err
			}
			return database.Audit(ctx, tx, "telegram_bot", "", "connected", nil, map[string]any{"username": me.Username})
		})
		if err != nil {
			return err
		}
	}
	if req.Enabled != nil || req.WelcomeText != nil || req.Notify != nil {
		err := h.DB.Tx(ctx, func(tx pgx.Tx) error {
			var notify []byte
			if req.Notify != nil {
				n := map[string]bool{}
				for _, t := range notifyTypes {
					if v, ok := req.Notify[t]; ok {
						n[t] = v
					}
				}
				notify, _ = json.Marshal(n)
			}
			tag, err := tx.Exec(ctx, `UPDATE tenant_bots SET enabled=coalesce($1, enabled), welcome_text=coalesce(NULLIF($2, '#keep'), welcome_text),
				notify=coalesce($3::jsonb, notify), updated_at=NOW()`, req.Enabled, keepOr(req.WelcomeText), notify)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return httpx.BadRequest("BOT_NOT_CONNECTED", "Сначала подключите бота по токену")
			}
			return database.Audit(ctx, tx, "telegram_bot", "", "settings_changed", nil, req.Notify)
		})
		if err != nil {
			return err
		}
	}
	return h.getBot(w, r)
}

// keepOr: nil — не менять приветствие; пустая строка — сбросить на стандартное.
func keepOr(s *string) *string {
	if s == nil {
		k := "#keep"
		return &k
	}
	t := strings.TrimSpace(*s)
	return &t
}

func (h *Handler) deleteBot(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	var token string
	err := h.DB.Tx(ctx, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `DELETE FROM tenant_bots RETURNING token`).Scan(&token); err != nil {
			return err
		}
		return database.Audit(ctx, tx, "telegram_bot", "", "disconnected", nil, nil)
	})
	if err != nil {
		return err
	}
	telegram.Call(ctx, token, "setChatMenuButton", map[string]any{"menu_button": map[string]string{"type": "default"}}, nil)
	return httpx.NoContent(w)
}

// ---------------------------------------------------------------- доступ преподавателя

func (h *Handler) employeeTelegram(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	out := map[string]any{}
	err := h.DB.Tx(ctx, func(tx pgx.Tx) error {
		var linked bool
		var invite *time.Time
		if err := tx.QueryRow(ctx, `SELECT coalesce((SELECT u.telegram_id IS NOT NULL FROM users u WHERE u.id=e.user_id), false),
				(SELECT max(expires_at) FROM telegram_invites i WHERE i.employee_id=e.id AND i.used_at IS NULL AND i.expires_at > NOW())
			FROM employees e WHERE e.id=$1`, id).Scan(&linked, &invite); err != nil {
			return err
		}
		out["linked"] = linked
		out["invite_expires_at"] = invite
		return nil
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, out)
}

// invite создаёт одноразовую ссылку на бота (7 дней). Если у преподавателя нет учётной записи,
// она создаётся с ролью «Преподаватель» — вход только через Telegram.
func (h *Handler) invite(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	p := auth.FromContext(r.Context())
	ctx := r.Context()
	if _, ok := h.Platform.For(ctx, h.DB.Sys, p.TenantID); !ok {
		return httpx.BadRequest("BOT_NOT_CONNECTED", "Подключите Telegram-бота школы в настройках")
	}
	var link string
	var expires time.Time
	err := h.DB.Tx(ctx, func(tx pgx.Tx) error {
		var userID *string
		var name, status string
		var email *string
		var branchIDs []string
		if err := tx.QueryRow(ctx, `SELECT user_id::text, full_name, email, status, branch_ids::text[] FROM employees WHERE id=$1 FOR UPDATE`, id).
			Scan(&userID, &name, &email, &status, &branchIDs); err != nil {
			return err
		}
		if status != "active" {
			return httpx.BadRequest("EMPLOYEE_ARCHIVED", "Сотрудник в архиве")
		}
		if userID == nil {
			uid, err := createTeacherUser(ctx, h.DB, tx, name, email, branchIDs)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE employees SET user_id=$1 WHERE id=$2`, uid, id); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM telegram_invites WHERE employee_id=$1 AND used_at IS NULL`, id); err != nil {
			return err
		}
		code := auth.RandomCode(24)
		expires = time.Now().Add(7 * 24 * time.Hour)
		if _, err := tx.Exec(ctx, `INSERT INTO telegram_invites (code, tenant_id, employee_id, created_by, expires_at) VALUES ($1, app_current_tenant(), $2, $3, $4)`,
			code, id, p.UserID, expires); err != nil {
			return err
		}
		link = "https://t.me/" + telegram.Username(ctx, tx, h.Platform.Username) + "?start=inv_" + code
		return database.Audit(ctx, tx, "employee", id, "telegram_invite_created", nil, nil)
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{"link": link, "expires_at": expires})
}

func createTeacherUser(ctx context.Context, db *database.DB, tx pgx.Tx, name string, email *string, branchIDs []string) (string, error) {
	// email уникален глобально; если он занят или не указан — служебный адрес (вход по паролю не используется)
	login := strings.ToLower(strings.TrimSpace(deref(email)))
	var taken bool
	if login != "" {
		db.Sys.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE lower(email)=$1)`, login).Scan(&taken)
	}
	if login == "" || taken {
		login = "tg-" + auth.RandomCode(12) + "@cabinet.local"
	}
	hash, err := auth.HashPassword(auth.RandomCode(32))
	if err != nil {
		return "", err
	}
	var uid string
	if err := tx.QueryRow(ctx, `INSERT INTO users (tenant_id, email, full_name, password_hash) VALUES (app_current_tenant(), $1, $2, $3) RETURNING id`,
		login, name, hash).Scan(&uid); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO user_roles (tenant_id, user_id, role_id) SELECT app_current_tenant(), $1, id FROM roles WHERE code='teacher'`, uid); err != nil {
		return "", err
	}
	for _, b := range branchIDs {
		if _, err := tx.Exec(ctx, `INSERT INTO user_branch_access (tenant_id, user_id, branch_id) VALUES (app_current_tenant(), $1, $2)`, uid, b); err != nil {
			return "", err
		}
	}
	return uid, nil
}

func (h *Handler) revoke(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	var userID *string
	err := h.DB.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM telegram_invites WHERE employee_id=$1 AND used_at IS NULL`, id); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT user_id::text FROM employees WHERE id=$1`, id).Scan(&userID); err != nil {
			return err
		}
		if userID != nil {
			if _, err := tx.Exec(ctx, `UPDATE users SET telegram_id=NULL WHERE id=$1`, *userID); err != nil {
				return err
			}
		}
		return database.Audit(ctx, tx, "employee", id, "telegram_revoked", nil, nil)
	})
	if err != nil {
		return err
	}
	if userID != nil {
		// открытые сессии Mini App завершаются (таблица sessions доступна только системной роли)
		h.DB.Sys.Exec(ctx, `UPDATE sessions SET revoked_at=NOW() WHERE user_id=$1 AND revoked_at IS NULL`, *userID)
	}
	return httpx.NoContent(w)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
