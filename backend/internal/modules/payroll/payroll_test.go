package payroll

import (
	"strings"
	"testing"
)

func TestApply(t *testing.T) {
	def := DefaultSettings()
	min3zero := def
	min3zero.MinStudents = 3
	min3amount := min3zero
	min3amount.BelowMin, min3amount.MinAmountTiyn = "min_amount", 200000
	coef150 := def
	coef150.Substitute, coef150.SubstituteValue = "coef", 150

	cases := []struct {
		name       string
		s          Settings
		payType    string
		rate       int64
		pct        int
		paid       int
		revenue    int64
		substitute bool
		want       int64
		formula    string
	}{
		{"per lesson", def, "per_lesson", 500000, 0, 4, 0, false, 500000, "Ставка за занятие 5 000 ₸"},
		{"hourly 90 min", def, "hourly", 400000, 0, 4, 0, false, 600000, "в час × 1,50 ч"},
		{"percent 40%", def, "percent", 4000, 0, 4, 2000000, false, 800000, "40% от стоимости урока 20 000 ₸"},
		{"mixed 5000 + 10%", def, "mixed", 500000, 1000, 4, 2000000, false, 700000, "+ 10% от стоимости урока 20 000 ₸"},
		{"mixed without percent = per lesson", def, "mixed", 500000, 0, 4, 2000000, false, 500000, "Ставка за занятие 5 000 ₸"},
		{"fixed salary: nothing per lesson", def, "fixed", 30000000, 0, 4, 0, false, 0, "Оклад"},
		{"no paid students", def, "per_lesson", 500000, 0, 0, 0, false, 0, "Нет учеников"},
		{"below minimum: zero", min3zero, "per_lesson", 500000, 0, 2, 0, false, 0, "меньше минимума 3: урок не оплачивается"},
		{"below minimum: min amount", min3amount, "per_lesson", 500000, 0, 2, 0, false, 200000, "минимальная оплата 2 000 ₸"},
		{"minimum reached", min3amount, "per_lesson", 500000, 0, 3, 0, false, 500000, "Ставка за занятие"},
		{"substitute coef 150%", coef150, "per_lesson", 500000, 0, 4, 0, true, 750000, "замена: 5 000 ₸ × 150% = 7 500 ₸"},
		{"coef ignored for own lesson", coef150, "per_lesson", 500000, 0, 4, 0, false, 500000, "Ставка за занятие 5 000 ₸"},
	}
	for _, c := range cases {
		minutes := 60
		if c.payType == "hourly" {
			minutes = 90
		}
		got, formula := Apply(c.s, c.payType, c.rate, c.pct, minutes, c.paid, c.revenue, c.substitute)
		if got != c.want || !strings.Contains(formula, c.formula) {
			t.Errorf("%s: got %d %q, want %d containing %q", c.name, got, formula, c.want, c.formula)
		}
	}
}
