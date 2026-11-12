package payroll

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"emp/internal/platform/auth"
	"emp/internal/platform/database"
	"emp/internal/platform/httpx"
	mw "emp/internal/platform/middleware"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/xuri/excelize/v2"
)

type Handler struct{ DB *database.DB }

func (h *Handler) Routes(r chi.Router) {
	r.With(mw.Perm("payroll.view")).Get("/payroll", httpx.Handler(h.report).ServeHTTP)
	r.With(mw.Perm("payroll.view")).Get("/payroll/export", httpx.Handler(h.export).ServeHTTP)
	r.With(mw.Perm("payroll.view")).Post("/payroll/recalc", httpx.Handler(h.recalc).ServeHTTP)
	// кабинет преподавателя: те же расчёты, только свои занятия
	r.Get("/teacher/payroll", httpx.Handler(h.teacher).ServeHTTP)
}

type Filter struct {
	From, To  string // YYYY-MM-DD включительно, в часовом поясе школы
	TeacherID string
	Branches  []string // nil — все филиалы
}

type LessonRow struct {
	LessonID      string    `json:"lesson_id"`
	StartsAt      time.Time `json:"starts_at"`
	Minutes       int       `json:"minutes"`
	Group         string    `json:"group"`
	Course        string    `json:"course"`
	Branch        string    `json:"branch"`
	TeacherID     string    `json:"teacher_id"`
	Teacher       string    `json:"teacher"`
	SubstituteFor *string   `json:"substitute_for"`
	Topic         *string   `json:"topic"`
	StudentsTotal int       `json:"students_total"`
	StudentsPaid  int       `json:"students_paid"`
	Present       int       `json:"present"`
	Late          int       `json:"late"`
	Absent        int       `json:"absent"`
	Excused       int       `json:"excused"`
	Trial         int       `json:"trial"`
	PayType       string    `json:"pay_type"`
	RateTiyn      int64     `json:"rate_tiyn"`
	RevenueTiyn   int64     `json:"revenue_tiyn"`
	AmountTiyn    int64     `json:"amount_tiyn"`
	Formula       string    `json:"formula"`
	CalculatedAt  time.Time `json:"calculated_at"`
}

type TeacherSum struct {
	TeacherID     string `json:"teacher_id"`
	Teacher       string `json:"teacher"`
	PayType       string `json:"pay_type"`  // текущие условия из карточки
	RateTiyn      int64  `json:"rate_tiyn"` // текущая ставка
	Lessons       int    `json:"lessons"`
	PaidLessons   int    `json:"paid_lessons"`
	Minutes       int    `json:"minutes"`
	Substitutions int    `json:"substitutions"`
	StudentsPaid  int    `json:"students_paid"`
	Present       int    `json:"present"`
	Late          int    `json:"late"`
	Absent        int    `json:"absent"`
	Excused       int    `json:"excused"`
	Trial         int    `json:"trial"`
	AmountTiyn    int64  `json:"amount_tiyn"`
	SalaryTiyn    int64  `json:"salary_tiyn"` // оклад в месяц (pay_type = fixed)
}

type Report struct {
	From     string       `json:"from"`
	To       string       `json:"to"`
	Teachers []TeacherSum `json:"teachers"`
	Lessons  []LessonRow  `json:"lessons"`
	Total    TeacherSum   `json:"total"`
}

// Build считает отчёт за период. Проведённые занятия без начисления (закрытые до появления модуля) досчитываются.
func Build(ctx context.Context, tx pgx.Tx, f Filter) (Report, error) {
	rep := Report{From: f.From, To: f.To, Teachers: []TeacherSum{}, Lessons: []LessonRow{}}
	var tz string
	tx.QueryRow(ctx, `SELECT timezone FROM tenants WHERE id = app_current_tenant()`).Scan(&tz)
	if tz == "" {
		tz = "Asia/Almaty"
	}
	period := `l.starts_at >= ($1::date)::timestamp AT TIME ZONE $3 AND l.starts_at < (($2::date) + 1)::timestamp AT TIME ZONE $3`
	rows, err := tx.Query(ctx, `SELECT l.id::text FROM lessons l WHERE l.status='done' AND l.teacher_id IS NOT NULL AND `+period+`
		AND NOT EXISTS (SELECT 1 FROM teacher_accruals a WHERE a.lesson_id=l.id) LIMIT 5000`, f.From, f.To, tz)
	if err != nil {
		return rep, err
	}
	missing, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return rep, err
	}
	for _, id := range missing {
		if err := Recalc(ctx, tx, id, false); err != nil {
			return rep, err
		}
	}
	rows, err = tx.Query(ctx, `SELECT a.lesson_id::text, a.starts_at, a.minutes, g.name, c.name, b.name, a.teacher_id::text, e.full_name, sf.full_name, l.topic,
			a.students_total, a.students_paid, a.present, a.late, a.absent, a.excused, a.trial, a.pay_type, a.rate_tiyn, a.revenue_tiyn, a.amount_tiyn, a.formula, a.calculated_at
		FROM teacher_accruals a JOIN lessons l ON l.id=a.lesson_id JOIN groups g ON g.id=a.group_id JOIN courses c ON c.id=g.course_id
		JOIN branches b ON b.id=a.branch_id JOIN employees e ON e.id=a.teacher_id LEFT JOIN employees sf ON sf.id=a.substitute_for
		WHERE `+periodA+` AND ($4 = '' OR a.teacher_id::text = $4) AND ($5::uuid[] IS NULL OR a.branch_id = ANY($5::uuid[]))
		ORDER BY e.full_name, a.starts_at`, f.From, f.To, tz, f.TeacherID, f.Branches)
	if err != nil {
		return rep, err
	}
	defer rows.Close()
	idx := map[string]int{}
	for rows.Next() {
		var x LessonRow
		if err := rows.Scan(&x.LessonID, &x.StartsAt, &x.Minutes, &x.Group, &x.Course, &x.Branch, &x.TeacherID, &x.Teacher, &x.SubstituteFor, &x.Topic,
			&x.StudentsTotal, &x.StudentsPaid, &x.Present, &x.Late, &x.Absent, &x.Excused, &x.Trial, &x.PayType, &x.RateTiyn, &x.RevenueTiyn, &x.AmountTiyn,
			&x.Formula, &x.CalculatedAt); err != nil {
			return rep, err
		}
		rep.Lessons = append(rep.Lessons, x)
		i, ok := idx[x.TeacherID]
		if !ok {
			i = len(rep.Teachers)
			idx[x.TeacherID] = i
			rep.Teachers = append(rep.Teachers, TeacherSum{TeacherID: x.TeacherID, Teacher: x.Teacher})
		}
		add(&rep.Teachers[i], x)
		add(&rep.Total, x)
	}
	if err := rows.Err(); err != nil {
		return rep, err
	}
	rows.Close()
	for i := range rep.Teachers {
		t := &rep.Teachers[i]
		tx.QueryRow(ctx, `SELECT pay_type, pay_rate_tiyn FROM employees WHERE id=$1`, t.TeacherID).Scan(&t.PayType, &t.RateTiyn)
		if t.PayType == "fixed" {
			t.SalaryTiyn = t.RateTiyn
			rep.Total.SalaryTiyn += t.RateTiyn
		}
	}
	return rep, nil
}

const periodA = `a.starts_at >= ($1::date)::timestamp AT TIME ZONE $3 AND a.starts_at < (($2::date) + 1)::timestamp AT TIME ZONE $3`

func add(t *TeacherSum, x LessonRow) {
	t.Lessons++
	if x.AmountTiyn > 0 {
		t.PaidLessons++
	}
	if x.SubstituteFor != nil {
		t.Substitutions++
	}
	t.Minutes += x.Minutes
	t.StudentsPaid += x.StudentsPaid
	t.Present += x.Present
	t.Late += x.Late
	t.Absent += x.Absent
	t.Excused += x.Excused
	t.Trial += x.Trial
	t.AmountTiyn += x.AmountTiyn
}

// period разбирает from/to; по умолчанию — текущий месяц.
func period(r *http.Request) (string, string, error) {
	q := r.URL.Query()
	now := time.Now()
	from, to := q.Get("from"), q.Get("to")
	if m := q.Get("month"); m != "" {
		t, err := time.Parse("2006-01", m)
		if err != nil {
			return "", "", httpx.BadRequest("INVALID_MONTH", "Месяц в формате ГГГГ-ММ")
		}
		from, to = t.Format("2006-01-02"), t.AddDate(0, 1, -1).Format("2006-01-02")
	}
	if from == "" {
		from = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
	}
	if to == "" {
		to = now.Format("2006-01-02")
	}
	f, err1 := time.Parse("2006-01-02", from)
	t, err2 := time.Parse("2006-01-02", to)
	if err1 != nil || err2 != nil || t.Before(f) || t.Sub(f) > 370*24*time.Hour {
		return "", "", httpx.BadRequest("INVALID_PERIOD", "Укажите период не больше года")
	}
	return from, to, nil
}

func (h *Handler) filter(r *http.Request) (Filter, error) {
	from, to, err := period(r)
	if err != nil {
		return Filter{}, err
	}
	p := auth.FromContext(r.Context())
	f := Filter{From: from, To: to, TeacherID: r.URL.Query().Get("teacher_id"), Branches: p.BranchFilter()}
	if b := r.URL.Query().Get("branch_id"); b != "" {
		if !p.CanBranch(b) {
			return f, httpx.Forbidden()
		}
		f.Branches = []string{b}
	}
	return f, nil
}

func (h *Handler) report(w http.ResponseWriter, r *http.Request) error {
	f, err := h.filter(r)
	if err != nil {
		return err
	}
	var rep Report
	if err := h.DB.Tx(r.Context(), func(tx pgx.Tx) (err error) { rep, err = Build(r.Context(), tx, f); return }); err != nil {
		return err
	}
	return httpx.OK(w, rep)
}

func (h *Handler) teacher(w http.ResponseWriter, r *http.Request) error {
	p := auth.FromContext(r.Context())
	if p.EmployeeID == "" {
		return httpx.NewError(403, "NOT_TEACHER", "Пользователь не связан с карточкой преподавателя")
	}
	from, to, err := period(r)
	if err != nil {
		return err
	}
	var rep Report
	err = h.DB.Tx(r.Context(), func(tx pgx.Tx) (err error) {
		rep, err = Build(r.Context(), tx, Filter{From: from, To: to, TeacherID: p.EmployeeID})
		return
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, rep)
}

// recalc применяет текущие ставки преподавателей к проведённым занятиям периода.
func (h *Handler) recalc(w http.ResponseWriter, r *http.Request) error {
	f, err := h.filter(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	n := 0
	err = h.DB.Tx(ctx, func(tx pgx.Tx) error {
		var tz string
		tx.QueryRow(ctx, `SELECT timezone FROM tenants WHERE id = app_current_tenant()`).Scan(&tz)
		rows, err := tx.Query(ctx, `SELECT l.id::text FROM lessons l WHERE l.status='done' AND l.teacher_id IS NOT NULL
			AND l.starts_at >= ($1::date)::timestamp AT TIME ZONE $3 AND l.starts_at < (($2::date) + 1)::timestamp AT TIME ZONE $3
			AND ($4 = '' OR l.teacher_id::text = $4) AND ($5::uuid[] IS NULL OR l.branch_id = ANY($5::uuid[]))`, f.From, f.To, tz, f.TeacherID, f.Branches)
		if err != nil {
			return err
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		for _, id := range ids {
			if err := Recalc(ctx, tx, id, true); err != nil {
				return err
			}
		}
		n = len(ids)
		return database.Audit(ctx, tx, "payroll", "", "recalculated", nil, map[string]any{"from": f.From, "to": f.To, "teacher_id": f.TeacherID, "lessons": n})
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]int{"lessons": n})
}

// export — выгрузка в Excel: лист «Итоги» по преподавателям и лист «Занятия» с расчётом по каждому уроку.
func (h *Handler) export(w http.ResponseWriter, r *http.Request) error {
	f, err := h.filter(r)
	if err != nil {
		return err
	}
	var rep Report
	var tz string
	err = h.DB.Tx(r.Context(), func(tx pgx.Tx) (err error) {
		tx.QueryRow(r.Context(), `SELECT timezone FROM tenants WHERE id = app_current_tenant()`).Scan(&tz)
		rep, err = Build(r.Context(), tx, f)
		return
	})
	if err != nil {
		return err
	}
	loc, _ := time.LoadLocation(tz)
	if loc == nil {
		loc = time.UTC
	}
	x := excelize.NewFile()
	defer x.Close()
	bold, _ := x.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	money, _ := x.NewStyle(&excelize.Style{NumFmt: 3})
	put := func(sheet string, row int, vals ...any) {
		for j, v := range vals {
			cell, _ := excelize.CoordinatesToCellName(j+1, row)
			x.SetCellValue(sheet, cell, v)
		}
	}
	tenge := func(t int64) float64 { return float64(t) / 100 }

	s1 := "Итоги"
	x.SetSheetName("Sheet1", s1)
	put(s1, 1, fmt.Sprintf("Начисления преподавателям за %s — %s", ruDate(rep.From), ruDate(rep.To)))
	x.SetCellStyle(s1, "A1", "A1", bold)
	head := []any{"Преподаватель", "Условия оплаты", "Ставка, ₸", "Проведено занятий", "Оплачиваемых", "Из них замен", "Часов", "Был", "Опоздал", "Не был", "Уваж.", "Пробных", "Начислено за занятия, ₸", "Оклад в месяц, ₸"}
	put(s1, 3, head...)
	x.SetCellStyle(s1, "A3", "N3", bold)
	row := 4
	for _, t := range append(rep.Teachers, rep.Total) {
		name, pt := t.Teacher, PayTypes[t.PayType]
		if name == "" {
			name, pt = "Итого", ""
		}
		put(s1, row, name, pt, rateCell(t.PayType, t.RateTiyn), t.Lessons, t.PaidLessons, t.Substitutions, float64(t.Minutes)/60,
			t.Present, t.Late, t.Absent, t.Excused, t.Trial, tenge(t.AmountTiyn), tenge(t.SalaryTiyn))
		row++
	}
	x.SetCellStyle(s1, fmt.Sprintf("A%d", row-1), fmt.Sprintf("N%d", row-1), bold)
	x.SetColWidth(s1, "A", "A", 30)
	x.SetColWidth(s1, "B", "N", 14)
	x.SetCellStyle(s1, "M4", fmt.Sprintf("N%d", row-1), money)

	s2 := "Занятия"
	x.NewSheet(s2)
	put(s2, 1, "Дата", "Время", "Минут", "Преподаватель", "Замена вместо", "Группа", "Курс", "Филиал", "Тема", "Отмечено", "Был", "Опоздал", "Не был", "Уваж.", "Пробных",
		"Оплачиваемых учеников", "Условия", "Ставка", "Стоимость урока, ₸", "Начислено, ₸", "Расчёт")
	x.SetCellStyle(s2, "A1", "U1", bold)
	for i, l := range rep.Lessons {
		st := l.StartsAt.In(loc)
		put(s2, i+2, st.Format("02.01.2006"), st.Format("15:04"), l.Minutes, l.Teacher, str(l.SubstituteFor), l.Group, l.Course, l.Branch, str(l.Topic),
			l.StudentsTotal, l.Present, l.Late, l.Absent, l.Excused, l.Trial, l.StudentsPaid, PayTypes[l.PayType], rateCell(l.PayType, l.RateTiyn),
			tenge(l.RevenueTiyn), tenge(l.AmountTiyn), l.Formula)
	}
	if n := len(rep.Lessons); n > 0 {
		x.SetCellStyle(s2, "S2", fmt.Sprintf("T%d", n+1), money)
	}
	x.SetColWidth(s2, "D", "I", 22)
	x.SetColWidth(s2, "U", "U", 50)

	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="payroll_`+rep.From+`_`+rep.To+`.xlsx"`)
	return x.Write(w)
}

func rateCell(payType string, rate int64) any {
	if payType == "percent" {
		return percent(rate) + "%"
	}
	return float64(rate) / 100
}

func ruDate(d string) string {
	t, err := time.Parse("2006-01-02", d)
	if err != nil {
		return d
	}
	return t.Format("02.01.2006")
}

func str(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
