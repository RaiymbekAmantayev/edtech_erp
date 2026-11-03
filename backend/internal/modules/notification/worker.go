// Пакет notification — обработка outbox, уведомления родителям и Telegram-бот (ТЗ разделы 3.4, 19, 20.1).
package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"emp/internal/modules/schedule"
	"emp/internal/platform/database"

	"github.com/jackc/pgx/v5"
)

type Worker struct {
	DB       *database.DB
	BotToken string
	client   *http.Client
}

func (w *Worker) Run(ctx context.Context) {
	w.client = &http.Client{Timeout: 40 * time.Second}
	slog.Info("worker started", "telegram", w.BotToken != "")
	if w.BotToken != "" {
		go w.pollTelegram(ctx)
	}
	go w.every(ctx, time.Hour, "schedule horizon", w.extendSchedules)
	go w.every(ctx, time.Hour, "subscription statuses", w.subscriptionStatuses)
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if err := w.processOutbox(ctx); err != nil {
				slog.Error("outbox", "err", err)
			}
		}
	}
}

func (w *Worker) every(ctx context.Context, d time.Duration, name string, fn func(context.Context) error) {
	for {
		if err := fn(ctx); err != nil {
			slog.Error("job failed", "job", name, "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(d):
		}
	}
}

// inTenant выполняет задачу по одной организации через пул приложения — под RLS, как обычный запрос.
func (w *Worker) inTenant(ctx context.Context, tenantID string, fn func(tx pgx.Tx) error) error {
	return w.DB.InTenant(ctx, tenantID, fn)
}

func (w *Worker) tenants(ctx context.Context) ([]string, error) {
	rows, err := w.DB.Sys.Query(ctx, `SELECT id::text FROM tenants WHERE status <> 'suspended'`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

func (w *Worker) extendSchedules(ctx context.Context) error {
	ids, err := w.tenants(ctx)
	if err != nil {
		return err
	}
	total := 0
	for _, t := range ids {
		err := w.inTenant(ctx, t, func(tx pgx.Tx) error {
			n, err := schedule.ExtendAll(ctx, tx)
			total += n
			return err
		})
		if err != nil {
			slog.Error("extend schedules", "tenant", t, "err", err)
		}
	}
	slog.Info("schedule horizon extended", "lessons_created", total)
	return nil
}

// subscriptionStatuses: автоматическая разморозка, истечение срока абонементов.
func (w *Worker) subscriptionStatuses(ctx context.Context) error {
	_, err := w.DB.Sys.Exec(ctx, `
		UPDATE subscriptions s SET status = CASE WHEN EXISTS (SELECT 1 FROM subscription_freezes f WHERE f.subscription_id=s.id AND CURRENT_DATE BETWEEN f.starts_on AND f.ends_on)
			THEN 'frozen' ELSE 'active' END
		WHERE s.status IN ('active','frozen')`)
	if err != nil {
		return err
	}
	_, err = w.DB.Sys.Exec(ctx, `UPDATE subscriptions SET status='expired' WHERE status IN ('active','frozen') AND ends_on < CURRENT_DATE`)
	return err
}

// ---------------------------------------------------------------- outbox

type event struct {
	ID       int64
	TenantID string
	Type     string
	Payload  map[string]any
	Attempts int
}

var errQuiet = errors.New("quiet hours")

func (w *Worker) processOutbox(ctx context.Context) error {
	return pgx.BeginFunc(ctx, w.DB.Sys, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id, tenant_id, event_type, payload, attempts FROM outbox_events
			WHERE processed_at IS NULL AND attempts < 10 ORDER BY id LIMIT 50 FOR UPDATE SKIP LOCKED`)
		if err != nil {
			return err
		}
		var events []event
		for rows.Next() {
			var e event
			rows.Scan(&e.ID, &e.TenantID, &e.Type, &e.Payload, &e.Attempts)
			events = append(events, e)
		}
		rows.Close()
		for _, e := range events {
			err := w.handle(ctx, e)
			switch {
			case errors.Is(err, errQuiet):
				// тихие часы: событие остаётся в очереди до утра
			case err != nil:
				tx.Exec(ctx, `UPDATE outbox_events SET attempts=attempts+1, last_error=$2 WHERE id=$1`, e.ID, err.Error())
			default:
				tx.Exec(ctx, `UPDATE outbox_events SET processed_at=NOW(), attempts=attempts+1 WHERE id=$1`, e.ID)
			}
		}
		return nil
	})
}

type recipient struct {
	ParentID, Name string
	ChatID         *int64
	Locale         string
}

// handle — обработчики идемпотентны: повторная доставка не создаёт дублей в журнале (проверка по event id).
func (w *Worker) handle(ctx context.Context, e event) error {
	var tz string
	var quiet map[string]string
	w.DB.Sys.QueryRow(ctx, `SELECT timezone, coalesce(settings->'quiet_hours', '{}') FROM tenants WHERE id=$1`, e.TenantID).Scan(&tz, &quiet)
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc, _ = time.LoadLocation("Asia/Almaty")
	}
	str := func(k string) string { s, _ := e.Payload[k].(string); return s }
	var studentIDs []string
	var text string
	switch e.Type {
	case "attendance.marked":
		var name, group string
		var starts time.Time
		w.DB.Sys.QueryRow(ctx, `SELECT s.full_name, g.name, l.starts_at FROM students s, lessons l JOIN groups g ON g.id=l.group_id WHERE s.id=$1 AND l.id=$2`,
			str("student_id"), str("lesson_id")).Scan(&name, &group, &starts)
		status := str("status")
		if status == "absent" {
			text = fmt.Sprintf("❗ %s не пришёл(ла) на занятие «%s» %s.", name, group, starts.In(loc).Format("02.01 в 15:04"))
		} else {
			text = fmt.Sprintf("⏰ %s опоздал(а) на занятие «%s» %s.", name, group, starts.In(loc).Format("02.01 в 15:04"))
		}
		studentIDs = []string{str("student_id")}
	case "lesson.cancelled", "lesson.rescheduled":
		var group string
		var starts time.Time
		w.DB.Sys.QueryRow(ctx, `SELECT g.name, l.starts_at FROM lessons l JOIN groups g ON g.id=l.group_id WHERE l.id=$1`, str("lesson_id")).Scan(&group, &starts)
		if e.Type == "lesson.cancelled" {
			text = fmt.Sprintf("Занятие «%s» %s отменено.", group, starts.In(loc).Format("02.01 в 15:04"))
			if r := str("reason"); r != "" {
				text += " Причина: " + r
			}
		} else {
			text = fmt.Sprintf("Занятие «%s» перенесено на %s.", group, starts.In(loc).Format("02.01 в 15:04"))
		}
		rows, err := w.DB.Sys.Query(ctx, `SELECT gs.student_id::text FROM lessons l JOIN group_students gs ON gs.group_id=l.group_id AND gs.left_at IS NULL WHERE l.id=$1`, str("lesson_id"))
		if err != nil {
			return err
		}
		studentIDs, _ = pgx.CollectRows(rows, pgx.RowTo[string])
	case "subscription.expiring":
		var name, sub string
		w.DB.Sys.QueryRow(ctx, `SELECT s.full_name, sb.name FROM subscriptions sb JOIN students s ON s.id=sb.student_id WHERE sb.id=$1`, str("subscription_id")).Scan(&name, &sub)
		left, _ := e.Payload["lessons_left"].(float64)
		text = fmt.Sprintf("Абонемент «%s» (%s): осталось занятий — %d. Продлите, чтобы не прерывать обучение.", sub, name, int(left))
		studentIDs = []string{str("student_id")}
	case "payment.completed":
		var name string
		var balance int64
		w.DB.Sys.QueryRow(ctx, `SELECT s.full_name, coalesce(b.balance_tiyn,0) FROM students s LEFT JOIN student_balances b ON b.student_id=s.id WHERE s.id=$1`, str("student_id")).Scan(&name, &balance)
		amount, _ := e.Payload["amount_tiyn"].(float64)
		text = fmt.Sprintf("✅ Оплата %s получена (%s). Баланс: %s.", Money(int64(amount)), name, Money(balance))
		studentIDs = []string{str("student_id")}
	case "group.seat_freed":
		// ответственный получает уведомление при освобождении места, если есть лист ожидания
		var waiting int
		w.DB.Sys.QueryRow(ctx, `SELECT count(*) FROM group_waitlist WHERE group_id=$1`, str("group_id")).Scan(&waiting)
		if waiting > 0 {
			w.log(ctx, e, nil, "web", "В группе освободилось место — в листе ожидания "+fmt.Sprint(waiting), "sent", "")
		}
		return nil
	default:
		return nil // события без уведомлений (student.created, lead.created …) — только фиксация
	}
	if len(studentIDs) == 0 || text == "" {
		return nil
	}
	if inQuiet(time.Now().In(loc), quiet) {
		return errQuiet
	}
	rows, err := w.DB.Sys.Query(ctx, `SELECT DISTINCT p.id, p.full_name, p.telegram_chat_id, p.locale FROM student_parents sp JOIN parents p ON p.id=sp.parent_id
		WHERE sp.student_id = ANY($1::uuid[]) AND p.archived_at IS NULL`, studentIDs)
	if err != nil {
		return err
	}
	var recs []recipient
	for rows.Next() {
		var r recipient
		rows.Scan(&r.ParentID, &r.Name, &r.ChatID, &r.Locale)
		recs = append(recs, r)
	}
	rows.Close()
	for _, r := range recs {
		// идемпотентность: если по этому событию родителю уже отправлено — пропускаем
		var done bool
		w.DB.Sys.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM notification_logs WHERE parent_id=$1 AND event_type=$2 AND status='sent')`, r.ParentID, fmt.Sprintf("%s#%d", e.Type, e.ID)).Scan(&done)
		if done {
			continue
		}
		// приоритет каналов: Telegram → (WhatsApp/SMS — этап 7). Ошибка доставки не блокирует бизнес-операции.
		if r.ChatID == nil {
			w.log(ctx, e, &r.ParentID, "telegram", text, "skipped", "Telegram не привязан")
			continue
		}
		if w.BotToken == "" {
			w.log(ctx, e, &r.ParentID, "telegram", text, "skipped", "TELEGRAM_BOT_TOKEN не задан")
			continue
		}
		if err := w.send(ctx, *r.ChatID, text); err != nil {
			w.log(ctx, e, &r.ParentID, "telegram", text, "failed", err.Error())
			return err // повторная попытка позже
		}
		w.log(ctx, e, &r.ParentID, "telegram", text, "sent", "")
	}
	return nil
}

func (w *Worker) log(ctx context.Context, e event, parentID *string, channel, body, status, errText string) {
	w.DB.Sys.Exec(ctx, `INSERT INTO notification_logs (tenant_id, parent_id, channel, event_type, body, status, error) VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''))`,
		e.TenantID, parentID, channel, fmt.Sprintf("%s#%d", e.Type, e.ID), body, status, errText)
}

func inQuiet(now time.Time, q map[string]string) bool {
	from, to := q["from"], q["to"]
	if from == "" || to == "" {
		return false
	}
	cur := now.Format("15:04")
	if from > to { // через полночь: 22:00–08:00
		return cur >= from || cur < to
	}
	return cur >= from && cur < to
}

// Money форматирует тиын как «25 000 ₸».
func Money(tiyn int64) string {
	neg := tiyn < 0
	if neg {
		tiyn = -tiyn
	}
	s := fmt.Sprint(tiyn / 100)
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteRune(' ')
		}
		b.WriteRune(r)
	}
	out := b.String() + " ₸"
	if neg {
		return "−" + out
	}
	return out
}

// ---------------------------------------------------------------- Telegram

func (w *Worker) api(ctx context.Context, method string, body any, out any) error {
	b, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.telegram.org/bot"+w.BotToken+"/"+method, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := w.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var res struct {
		OK          bool            `json:"ok"`
		Description string          `json:"description"`
		Result      json.RawMessage `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return err
	}
	if !res.OK {
		return errors.New(res.Description)
	}
	if out != nil {
		return json.Unmarshal(res.Result, out)
	}
	return nil
}

func (w *Worker) send(ctx context.Context, chatID int64, text string) error {
	return w.api(ctx, "sendMessage", map[string]any{"chat_id": chatID, "text": text}, nil)
}

// pollTelegram — long polling: /start <код> привязывает родителя к чату (Telegram ID заполняется только через бота).
func (w *Worker) pollTelegram(ctx context.Context) {
	offset := int64(0)
	for ctx.Err() == nil {
		var updates []struct {
			UpdateID int64 `json:"update_id"`
			Message  *struct {
				Text string `json:"text"`
				Chat struct {
					ID int64 `json:"id"`
				} `json:"chat"`
			} `json:"message"`
		}
		if err := w.api(ctx, "getUpdates", map[string]any{"offset": offset, "timeout": 30}, &updates); err != nil {
			slog.Warn("telegram getUpdates", "err", err)
			time.Sleep(5 * time.Second)
			continue
		}
		for _, u := range updates {
			offset = u.UpdateID + 1
			if u.Message == nil {
				continue
			}
			code, ok := strings.CutPrefix(strings.TrimSpace(u.Message.Text), "/start ")
			if !ok {
				w.send(ctx, u.Message.Chat.ID, "Здравствуйте! Чтобы получать уведомления, откройте ссылку-приглашение от учебного центра.")
				continue
			}
			var name, tenantID string
			err := w.DB.Sys.QueryRow(ctx, `UPDATE parents SET telegram_chat_id=$2, preferred_channel='telegram' WHERE telegram_link_code=$1 RETURNING full_name, tenant_id`,
				strings.TrimSpace(code), u.Message.Chat.ID).Scan(&name, &tenantID)
			if err != nil {
				w.send(ctx, u.Message.Chat.ID, "Ссылка недействительна. Попросите администратора центра отправить новую.")
				continue
			}
			w.DB.Sys.Exec(ctx, `INSERT INTO audit_logs (tenant_id, entity_type, action, new_values) VALUES ($1,'parent','telegram_linked',$2)`, tenantID, map[string]any{"chat": "linked"})
			w.send(ctx, u.Message.Chat.ID, "Готово, "+name+"! Вы будете получать уведомления о посещаемости, расписании и оплатах.")
		}
	}
}
