package notification

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"emp/internal/platform/telegram"

	"github.com/jackc/pgx/v5"
)

// ---------------------------------------------------------------- Telegram-боты
//
// У каждой школы может быть свой бот (tenant_bots); школы без своего бота используют общий бот платформы.
// Worker опрашивает каждого бота через getUpdates и обрабатывает:
//   /start <код родителя>  — привязка родителя к уведомлениям;
//   /start inv_<код>       — приглашение преподавателя в кабинет (одноразовое);
//   любое другое сообщение — приветствие школы и кнопка «Открыть кабинет».

type bot struct {
	TenantID string // пусто — общий бот платформы
	Token    string
	ID       int64
	Username string
}

func (w *Worker) reloadBots(ctx context.Context) {
	list := map[int64]bot{}
	if w.BotToken != "" {
		list[telegram.BotID(w.BotToken)] = bot{Token: w.BotToken, ID: telegram.BotID(w.BotToken)}
	}
	rows, err := w.DB.Sys.Query(ctx, `SELECT tenant_id::text, token, bot_id, username FROM tenant_bots WHERE enabled`)
	if err != nil {
		slog.Error("load telegram bots", "err", err)
		return
	}
	for rows.Next() {
		var b bot
		if err := rows.Scan(&b.TenantID, &b.Token, &b.ID, &b.Username); err == nil {
			list[b.ID] = b
		}
	}
	rows.Close()

	w.mu.Lock()
	defer w.mu.Unlock()
	if w.pollers == nil {
		w.pollers = map[string]context.CancelFunc{}
	}
	active := map[string]bool{}
	for _, b := range list {
		active[b.Token] = true
		if _, ok := w.pollers[b.Token]; !ok {
			pctx, cancel := context.WithCancel(ctx)
			w.pollers[b.Token] = cancel
			go w.poll(pctx, b)
		}
	}
	for token, cancel := range w.pollers {
		if !active[token] {
			cancel() // бот отключён или токен заменён
			delete(w.pollers, token)
		}
	}
	w.bots = list
}

// botFor — бот, через которого привязан чат родителя; NULL — привязка через общий бот (до появления своих ботов).
func (w *Worker) botFor(botID *int64) (bot, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	id := telegram.BotID(w.BotToken)
	if botID != nil {
		id = *botID
	}
	b, ok := w.bots[id]
	return b, ok && id != 0
}

var notifyKind = map[string]string{
	"attendance.marked": "attendance", "lesson.cancelled": "lessons", "lesson.rescheduled": "lessons",
	"subscription.expiring": "subscription", "payment.completed": "payment",
}

func (w *Worker) notifyEnabled(ctx context.Context, tenantID, eventType string) bool {
	var notify map[string]bool
	if err := w.DB.Sys.QueryRow(ctx, `SELECT notify FROM tenant_bots WHERE tenant_id=$1`, tenantID).Scan(&notify); err != nil {
		return true
	}
	v, set := notify[notifyKind[eventType]]
	return !set || v
}

func (w *Worker) send(ctx context.Context, token string, chatID int64, text string, markup any) error {
	body := map[string]any{"chat_id": chatID, "text": text}
	if markup != nil {
		body["reply_markup"] = markup
	}
	return telegram.Call(ctx, token, "sendMessage", body, nil)
}

type update struct {
	UpdateID int64 `json:"update_id"`
	Message  *struct {
		Text string `json:"text"`
		Chat struct {
			ID   int64  `json:"id"`
			Type string `json:"type"`
		} `json:"chat"`
		From struct {
			ID int64 `json:"id"`
		} `json:"from"`
	} `json:"message"`
}

func (w *Worker) poll(ctx context.Context, b bot) {
	offset := int64(0)
	for ctx.Err() == nil {
		var updates []update
		if err := telegram.Call(ctx, b.Token, "getUpdates", map[string]any{"offset": offset, "timeout": 30, "allowed_updates": []string{"message"}}, &updates); err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Warn("telegram getUpdates", "bot", b.Username, "err", err)
			if b.TenantID != "" && !errors.Is(err, telegram.ErrNetwork) {
				w.DB.Sys.Exec(ctx, `UPDATE tenant_bots SET last_error=$2 WHERE tenant_id=$1`, b.TenantID, err.Error())
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(10 * time.Second):
			}
			continue
		}
		for _, u := range updates {
			offset = u.UpdateID + 1
			if u.Message == nil || u.Message.Chat.Type != "private" {
				continue
			}
			w.handleMessage(ctx, b, u.Message.Chat.ID, u.Message.From.ID, strings.TrimSpace(u.Message.Text))
		}
	}
}

func (w *Worker) handleMessage(ctx context.Context, b bot, chatID, userID int64, text string) {
	payload := ""
	if rest, ok := strings.CutPrefix(text, "/start"); ok {
		payload = strings.TrimSpace(rest)
	}
	switch {
	case strings.HasPrefix(payload, "inv_"):
		w.acceptInvite(ctx, b, chatID, userID, strings.TrimPrefix(payload, "inv_"))
	case payload != "":
		w.linkParent(ctx, b, chatID, payload)
	default:
		w.welcome(ctx, b, chatID)
	}
}

func (w *Worker) cabinetButton(tenantID string) any {
	if tenantID == "" || !strings.HasPrefix(w.PublicURL, "https://") {
		return nil // Mini App открывается только по HTTPS
	}
	return telegram.OpenButton(telegram.MiniAppURL(w.PublicURL, tenantID))
}

func (w *Worker) welcome(ctx context.Context, b bot, chatID int64) {
	if b.TenantID == "" {
		w.send(ctx, b.Token, chatID, "Здравствуйте! Чтобы получать уведомления, откройте ссылку-приглашение от учебного центра.", nil)
		return
	}
	var school string
	var custom *string
	w.DB.Sys.QueryRow(ctx, `SELECT t.name, tb.welcome_text FROM tenants t JOIN tenant_bots tb ON tb.tenant_id=t.id WHERE t.id=$1`, b.TenantID).Scan(&school, &custom)
	text := "Здравствуйте! Это бот учебного центра «" + school + "».\n\n" +
		"Ученики и родители: нажмите «Открыть кабинет» и войдите по своему ИИН (пароль при первом входе — 12345).\n" +
		"Преподаватели: откройте ссылку-приглашение от администратора."
	if custom != nil && *custom != "" {
		text = *custom
	}
	w.send(ctx, b.Token, chatID, text, w.cabinetButton(b.TenantID))
}

// linkParent — привязка по постоянной ссылке из карточки родителя. Бот школы принимает только коды своей школы.
func (w *Worker) linkParent(ctx context.Context, b bot, chatID int64, code string) {
	var name, tenantID string
	err := w.DB.Sys.QueryRow(ctx, `UPDATE parents SET telegram_chat_id=$2, telegram_bot_id=$3, preferred_channel='telegram'
		WHERE telegram_link_code=$1 AND ($4 = '' OR tenant_id::text = $4) RETURNING full_name, tenant_id::text`,
		code, chatID, b.ID, b.TenantID).Scan(&name, &tenantID)
	if err != nil {
		w.send(ctx, b.Token, chatID, "Ссылка недействительна. Попросите администратора центра отправить новую.", nil)
		return
	}
	w.DB.Sys.Exec(ctx, `INSERT INTO audit_logs (tenant_id, entity_type, action, new_values) VALUES ($1,'parent','telegram_linked',$2)`, tenantID, map[string]any{"bot": b.Username})
	w.send(ctx, b.Token, chatID, "Готово, "+name+"! Вы будете получать уведомления о посещаемости, расписании и оплатах.", w.cabinetButton(tenantID))
}

// acceptInvite привязывает Telegram преподавателя к его учётной записи; приглашение одноразовое.
func (w *Worker) acceptInvite(ctx context.Context, b bot, chatID, userID int64, code string) {
	var tenantID, name string
	err := pgx.BeginFunc(ctx, w.DB.Sys, func(tx pgx.Tx) error {
		var employeeID string
		var userRef *string
		err := tx.QueryRow(ctx, `SELECT i.tenant_id::text, i.employee_id::text, e.user_id::text, e.full_name FROM telegram_invites i JOIN employees e ON e.id=i.employee_id
			WHERE i.code=$1 AND i.used_at IS NULL AND i.expires_at > NOW() AND e.status='active' FOR UPDATE OF i`, code).Scan(&tenantID, &employeeID, &userRef, &name)
		if err != nil {
			return err
		}
		if (b.TenantID != "" && b.TenantID != tenantID) || userRef == nil {
			return pgx.ErrNoRows
		}
		if _, err := tx.Exec(ctx, `UPDATE users SET telegram_id=NULL WHERE tenant_id=$1 AND telegram_id=$2`, tenantID, userID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE users SET telegram_id=$2 WHERE id=$1`, *userRef, userID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE telegram_invites SET used_at=NOW() WHERE code=$1`, code); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO audit_logs (tenant_id, entity_type, entity_id, action) VALUES ($1,'employee',$2,'telegram_linked')`, tenantID, employeeID)
		return err
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Error("telegram invite", "err", err)
		}
		w.send(ctx, b.Token, chatID, "Приглашение недействительно или устарело. Попросите администратора отправить новое.", nil)
		return
	}
	w.send(ctx, b.Token, chatID, "Готово, "+name+"! Доступ к кабинету преподавателя открыт: расписание, отметка посещаемости и статистика.", w.cabinetButton(tenantID))
}
