// Пакет payroll — начисления преподавателям за проведённые занятия.
// Начисление хранится по каждому уроку (teacher_accruals) и пересчитывается при правке отметок,
// поэтому отчёт администратора, выгрузка и кабинет преподавателя показывают одни и те же цифры.
package payroll

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var PayTypes = map[string]string{"fixed": "Оклад", "per_lesson": "За занятие", "hourly": "Почасовая", "percent": "Процент", "mixed": "Комбинированная"}

// Settings — настройки расчёта зарплаты школы (tenants.settings -> 'payroll').
// Какие статусы посещения оплачиваются преподавателю, хранится отдельно: attendance_charge_rules.pay_teacher.
type Settings struct {
	ShowToTeachers  bool   `json:"show_to_teachers"` // показывать начисления в кабинете преподавателя (можно переопределить в карточке)
	MinStudents     int    `json:"min_students"`     // минимум оплачиваемых учеников на уроке, 0 — без ограничения
	BelowMin        string `json:"below_min"`        // если учеников меньше минимума: zero — не начислять, min_amount — фиксированная сумма
	MinAmountTiyn   int64  `json:"min_amount_tiyn"`
	Substitute      string `json:"substitute"`       // оплата замены: own — по ставке заменяющего, original — по ставке основного преподавателя,
	SubstituteValue int64  `json:"substitute_value"` // fixed — фиксированная сумма за урок (тиын), coef — процент от начисления по своей ставке (100 = как есть)
}

var (
	BelowMinModes   = []string{"zero", "min_amount"}
	SubstituteModes = []string{"own", "original", "fixed", "coef"}
)

func DefaultSettings() Settings {
	return Settings{ShowToTeachers: true, BelowMin: "zero", Substitute: "own", SubstituteValue: 100}
}

// LoadSettings читает настройки школы текущей организации; незаданные поля берутся по умолчанию.
func LoadSettings(ctx context.Context, tx pgx.Tx) (Settings, error) {
	s := DefaultSettings()
	var raw []byte
	if err := tx.QueryRow(ctx, `SELECT settings->'payroll' FROM tenants WHERE id = app_current_tenant()`).Scan(&raw); err != nil && err != pgx.ErrNoRows {
		return s, err
	}
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &s); err != nil {
			return DefaultSettings(), nil
		}
	}
	return s, nil
}

// Visible — видит ли преподаватель свои начисления в кабинете: настройка в карточке, иначе настройка школы.
func Visible(ctx context.Context, tx pgx.Tx, employeeID string) (bool, error) {
	var own *bool
	if err := tx.QueryRow(ctx, `SELECT payroll_visible FROM employees WHERE id=$1`, employeeID).Scan(&own); err != nil {
		return false, err
	}
	if own != nil {
		return *own, nil
	}
	s, err := LoadSettings(ctx, tx)
	return s.ShowToTeachers, err
}

// Recalc пересчитывает начисление за урок. refreshRate=false сохраняет ставку, зафиксированную при закрытии урока
// (если преподаватель не менялся); true — берёт текущие условия оплаты из карточки преподавателя.
// Непроведённый урок или урок без преподавателя начисления не имеет.
func Recalc(ctx context.Context, tx pgx.Tx, lessonID string, refreshRate bool) error {
	var status string
	var teacherID, substituteFor *string
	var branchID, groupID string
	var starts, ends time.Time
	if err := tx.QueryRow(ctx, `SELECT status, teacher_id::text, substitute_for::text, branch_id::text, group_id::text, starts_at, ends_at FROM lessons WHERE id=$1`, lessonID).
		Scan(&status, &teacherID, &substituteFor, &branchID, &groupID, &starts, &ends); err != nil {
		return err
	}
	if status != "done" || teacherID == nil {
		_, err := tx.Exec(ctx, `DELETE FROM teacher_accruals WHERE lesson_id=$1`, lessonID)
		return err
	}
	set, err := LoadSettings(ctx, tx)
	if err != nil {
		return err
	}
	var payType string
	var rate int64
	var pct int
	var oldTeacher string
	err = tx.QueryRow(ctx, `SELECT pay_type, rate_tiyn, pay_percent, teacher_id::text FROM teacher_accruals WHERE lesson_id=$1`, lessonID).Scan(&payType, &rate, &pct, &oldTeacher)
	if err != nil && err != pgx.ErrNoRows {
		return err
	}
	if err == pgx.ErrNoRows || refreshRate || oldTeacher != *teacherID {
		// условия оплаты фиксируются при закрытии урока; для замены — по правилу из настроек школы
		payFrom := *teacherID
		if substituteFor != nil && set.Substitute == "original" {
			payFrom = *substituteFor
		}
		if err := tx.QueryRow(ctx, `SELECT pay_type, pay_rate_tiyn, pay_percent FROM employees WHERE id=$1`, payFrom).Scan(&payType, &rate, &pct); err != nil {
			return err
		}
		if substituteFor != nil && set.Substitute == "fixed" {
			payType, rate, pct = "per_lesson", set.SubstituteValue, 0
		}
	}
	// отметки урока и правила «оплачивается преподавателю»
	var c struct{ total, paid, present, late, absent, excused, trial int }
	if err := tx.QueryRow(ctx, `SELECT count(*),
			count(*) FILTER (WHERE coalesce(r.pay_teacher, false)),
			count(*) FILTER (WHERE a.status='present'), count(*) FILTER (WHERE a.status='late'),
			count(*) FILTER (WHERE a.status='absent'), count(*) FILTER (WHERE a.status='excused'), count(*) FILTER (WHERE a.status='trial')
		FROM attendance a LEFT JOIN attendance_charge_rules r ON r.status=a.status WHERE a.lesson_id=$1`, lessonID).
		Scan(&c.total, &c.paid, &c.present, &c.late, &c.absent, &c.excused, &c.trial); err != nil {
		return err
	}
	// выручка урока: занятия по абонементу (цена абонемента / число занятий) + разовые списания
	var revenue int64
	tx.QueryRow(ctx, `SELECT
			coalesce((SELECT sum(CASE WHEN s.lessons_total > 0 THEN s.price_tiyn / s.lessons_total ELSE 0 END * u.qty)
				FROM subscription_usages u JOIN subscriptions s ON s.id=u.subscription_id WHERE u.lesson_id=$1), 0)
			- coalesce((SELECT sum(amount_tiyn) FROM ledger_entries WHERE ref_type='lesson' AND ref_id=$1), 0)`, lessonID).Scan(&revenue)
	if revenue < 0 {
		revenue = 0
	}
	minutes := int(math.Round(ends.Sub(starts).Minutes()))
	amount, formula := Apply(set, payType, rate, pct, minutes, c.paid, revenue, substituteFor != nil)
	_, err = tx.Exec(ctx, `INSERT INTO teacher_accruals (lesson_id, tenant_id, teacher_id, branch_id, group_id, starts_at, minutes, pay_type, rate_tiyn, pay_percent,
			students_total, students_paid, present, late, absent, excused, trial, revenue_tiyn, amount_tiyn, formula, substitute_for, calculated_at)
		VALUES ($1, app_current_tenant(), $2, $3, $4, $5, $6, $7, $8, $20, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, NOW())
		ON CONFLICT (lesson_id) DO UPDATE SET teacher_id=EXCLUDED.teacher_id, branch_id=EXCLUDED.branch_id, group_id=EXCLUDED.group_id,
			starts_at=EXCLUDED.starts_at, minutes=EXCLUDED.minutes, pay_type=EXCLUDED.pay_type, rate_tiyn=EXCLUDED.rate_tiyn, pay_percent=EXCLUDED.pay_percent,
			students_total=EXCLUDED.students_total, students_paid=EXCLUDED.students_paid, present=EXCLUDED.present, late=EXCLUDED.late,
			absent=EXCLUDED.absent, excused=EXCLUDED.excused, trial=EXCLUDED.trial, revenue_tiyn=EXCLUDED.revenue_tiyn,
			amount_tiyn=EXCLUDED.amount_tiyn, formula=EXCLUDED.formula, substitute_for=EXCLUDED.substitute_for, calculated_at=NOW()`,
		lessonID, *teacherID, branchID, groupID, starts, minutes, payType, rate, c.total, c.paid, c.present, c.late, c.absent, c.excused, c.trial,
		revenue, amount, formula, substituteFor, pct)
	return err
}

// Apply — начисление за урок с учётом настроек школы: минимум учеников и коэффициент за замену.
func Apply(s Settings, payType string, rate int64, pct, minutes, paid int, revenue int64, substitute bool) (int64, string) {
	if payType == "fixed" {
		return Compute(payType, rate, pct, minutes, paid, revenue)
	}
	var amount int64
	var formula string
	switch {
	case s.MinStudents > 0 && paid < s.MinStudents && s.BelowMin == "min_amount":
		amount = s.MinAmountTiyn
		formula = fmt.Sprintf("Оплачиваемых учеников %d — меньше минимума %d: минимальная оплата %s", paid, s.MinStudents, Money(amount))
	case s.MinStudents > 0 && paid < s.MinStudents:
		formula = fmt.Sprintf("Оплачиваемых учеников %d — меньше минимума %d: урок не оплачивается (Настройки → Зарплата)", paid, s.MinStudents)
	case paid == 0:
		formula = "Нет учеников со статусом, который оплачивается преподавателю (Настройки → Зарплата)"
	default:
		amount, formula = Compute(payType, rate, pct, minutes, paid, revenue)
	}
	if substitute && s.Substitute == "coef" && s.SubstituteValue != 100 && amount > 0 {
		full := amount
		amount = int64(math.Round(float64(full) * float64(s.SubstituteValue) / 100))
		formula = fmt.Sprintf("%s; замена: %s × %d%% = %s", formula, Money(full), s.SubstituteValue, Money(amount))
	}
	return amount, formula
}

// Compute — сумма и понятная формула начисления за урок без учёта настроек школы.
// percent: ставка хранится как процент × 100 (4000 = 40%) от выручки урока.
// mixed: ставка за занятие + pct (процент × 100) от выручки урока.
func Compute(payType string, rate int64, pct, minutes, paid int, revenue int64) (int64, string) {
	if payType == "fixed" {
		return 0, "Оклад " + Money(rate) + " в месяц — за занятия отдельно не начисляется"
	}
	if paid == 0 {
		return 0, ""
	}
	switch payType {
	case "hourly":
		amount := int64(math.Round(float64(rate) * float64(minutes) / 60))
		return amount, fmt.Sprintf("%s в час × %s ч = %s", Money(rate), hours(minutes), Money(amount))
	case "percent":
		amount := int64(math.Round(float64(revenue) * float64(rate) / 10000))
		return amount, fmt.Sprintf("%s%% от стоимости урока %s = %s", percent(rate), Money(revenue), Money(amount))
	case "mixed":
		if pct <= 0 {
			return rate, "Ставка за занятие " + Money(rate)
		}
		part := int64(math.Round(float64(revenue) * float64(pct) / 10000))
		amount := rate + part
		return amount, fmt.Sprintf("Ставка за занятие %s + %s%% от стоимости урока %s (%s) = %s", Money(rate), percent(int64(pct)), Money(revenue), Money(part), Money(amount))
	default: // per_lesson
		return rate, "Ставка за занятие " + Money(rate)
	}
}

func hours(minutes int) string {
	h := float64(minutes) / 60
	if h == math.Trunc(h) {
		return fmt.Sprint(int(h))
	}
	return strings.Replace(fmt.Sprintf("%.2f", h), ".", ",", 1)
}

func percent(rate int64) string {
	if rate%100 == 0 {
		return fmt.Sprint(rate / 100)
	}
	return strings.Replace(fmt.Sprintf("%.2f", float64(rate)/100), ".", ",", 1)
}

// Money — сумма в тенге с разделителями разрядов.
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
		out = "−" + out
	}
	return out
}
