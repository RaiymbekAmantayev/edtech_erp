// Пакет notification — обработка outbox, уведомления родителям и Telegram-бот (ТЗ разделы 3.4, 19, 20.1).
package notification

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"emp/internal/modules/schedule"
	"emp/internal/platform/database"
	"emp/internal/platform/telegram"

	"github.com/jackc/pgx/v5"
)

type Worker struct {
	DB        *database.DB
	BotToken  string // общий бот платформы — для школ без своего бота
	PublicURL string // адрес веб-интерфейса — кнопка «Открыть кабинет»

	mu      sync.Mutex
	bots    map[int64]bot                 // активные боты по ID
	pollers map[string]context.CancelFunc // опрос getUpdates по токену
}

func (w *Worker) Run(ctx context.Context) {
	w.reloadBots(ctx)
	slog.Info("worker started", "telegram_bots", len(w.bots))
	go w.every(ctx, 30*time.Second, "telegram bots", func(ctx context.Context) error { w.reloadBots(ctx); return nil })
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
	BotID          *int64
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
	case "lesson.report":
		text = w.lessonReport(ctx, loc, str("lesson_id"), str("student_id"))
		studentIDs = []string{str("student_id")}
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
	if !w.notifyEnabled(ctx, e.TenantID, e.Type) {
		return nil // тип уведомлений выключен в настройках бота школы
	}
	rows, err := w.DB.Sys.Query(ctx, `SELECT DISTINCT p.id, p.full_name, p.telegram_chat_id, p.telegram_bot_id, p.locale FROM student_parents sp JOIN parents p ON p.id=sp.parent_id
		WHERE sp.student_id = ANY($1::uuid[]) AND p.archived_at IS NULL`, studentIDs)
	if err != nil {
		return err
	}
	var recs []recipient
	for rows.Next() {
		var r recipient
		rows.Scan(&r.ParentID, &r.Name, &r.ChatID, &r.BotID, &r.Locale)
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
		// chat_id действителен только для бота, через которого родитель привязался
		b, ok := w.botFor(r.BotID)
		if !ok {
			w.log(ctx, e, &r.ParentID, "telegram", text, "skipped", "Бот, через которого привязан Telegram, отключён")
			continue
		}
		if err := w.send(ctx, b.Token, *r.ChatID, text, nil); err != nil {
			if errors.Is(err, telegram.ErrNetwork) {
				return err // Telegram недоступен — повторная попытка позже
			}
			// чат не найден, бот заблокирован и т.п. — повтор не поможет, остальным родителям отправляем
			w.log(ctx, e, &r.ParentID, "telegram", text, "failed", err.Error())
			continue
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

// lessonReport — итог урока для родителей: тема, ДЗ и результаты тестов ребёнка. Пусто — уведомлять не о чем.
func (w *Worker) lessonReport(ctx context.Context, loc *time.Location, lessonID, studentID string) string {
	var name, group string
	var topic, homework, status *string
	var starts time.Time
	if err := w.DB.Sys.QueryRow(ctx, `SELECT s.full_name, g.name, l.starts_at, l.topic, l.homework,
			(SELECT a.status FROM attendance a WHERE a.lesson_id=l.id AND a.student_id=s.id)
		FROM lessons l JOIN groups g ON g.id=l.group_id, students s WHERE l.id=$1 AND s.id=$2`, lessonID, studentID).
		Scan(&name, &group, &starts, &topic, &homework, &status); err != nil {
		return ""
	}
	var b strings.Builder
	rows, err := w.DB.Sys.Query(ctx, `SELECT st.id::text, t.name, st.total_score::float8, st.max_score::float8, st.percent::float8, gr.name
		FROM student_tests st JOIN test_types t ON t.id=st.test_type_id LEFT JOIN test_grades gr ON gr.id=st.grade_id
		WHERE st.lesson_id=$1 AND st.student_id=$2 ORDER BY t.sort_order, t.name`, lessonID, studentID)
	if err == nil {
		type res struct {
			id, name            string
			total, max, percent *float64
			grade               *string
		}
		var list []res
		for rows.Next() {
			var r res
			if rows.Scan(&r.id, &r.name, &r.total, &r.max, &r.percent, &r.grade) == nil {
				list = append(list, r)
			}
		}
		rows.Close()
		for _, r := range list {
			b.WriteString("\n• " + r.name)
			if r.percent != nil {
				b.WriteString(fmt.Sprintf(": %s из %s (%s%%)", num(*r.total), num(*r.max), num(*r.percent)))
			}
			if r.grade != nil {
				b.WriteString(" — " + *r.grade)
			}
			var parts []string
			srows, err := w.DB.Sys.Query(ctx, `SELECT sk.name, sc.score::float8, sc.max_score::float8, gr.name FROM student_test_scores sc
				JOIN test_skills sk ON sk.id=sc.skill_id LEFT JOIN test_grades gr ON gr.id=sc.grade_id
				LEFT JOIN test_type_skills ts ON ts.skill_id=sc.skill_id AND ts.test_type_id=(SELECT test_type_id FROM student_tests WHERE id=$1)
				WHERE sc.test_id=$1 ORDER BY ts.sort_order NULLS LAST, sk.name`, r.id)
			if err == nil {
				for srows.Next() {
					var skill string
					var sc, mx *float64
					var gr *string
					if srows.Scan(&skill, &sc, &mx, &gr) != nil {
						continue
					}
					if gr != nil {
						parts = append(parts, skill+" "+*gr)
					} else if sc != nil && mx != nil {
						parts = append(parts, fmt.Sprintf("%s %s/%s", skill, num(*sc), num(*mx)))
					}
				}
				srows.Close()
			}
			if len(parts) > 1 {
				b.WriteString("\n   " + strings.Join(parts, " · "))
			}
		}
	}
	results := b.String()
	if topic == nil && homework == nil && results == "" {
		return ""
	}
	text := fmt.Sprintf("📘 Урок «%s», %s — %s", group, starts.In(loc).Format("02.01 15:04"), name)
	if status != nil && *status == "absent" {
		text += "\nНа уроке не был(а)."
	}
	if topic != nil {
		text += "\nТема: " + *topic
	}
	if homework != nil {
		text += "\n📝 ДЗ: " + *homework
	}
	if results != "" {
		text += "\n\nРезультаты:" + results
	}
	return text
}

func num(f float64) string {
	if f == float64(int64(f)) {
		return fmt.Sprintf("%d", int64(f))
	}
	return strings.Replace(fmt.Sprintf("%.1f", f), ".", ",", 1)
}
