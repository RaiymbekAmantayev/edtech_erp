// Пакет schedule — серии занятий, экземпляры занятий, конфликты, производственный календарь.
package schedule

import (
	"context"
	"slices"
	"time"

	_ "time/tzdata" // актуальная база часовых поясов внутри бинарника (Казахстан — UTC+5 с 01.03.2024)

	"github.com/jackc/pgx/v5"
)

// Horizon — экземпляры занятий генерируются на скользящий горизонт, а не на весь срок курса.
const Horizon = 8 * 7 * 24 * time.Hour

func isoWeekday(t time.Time) int { return (int(t.Weekday())+6)%7 + 1 }

type series struct {
	ID, GroupID, BranchID, TZ  string
	Weekdays                   []int32
	Start, End                 time.Duration
	TeacherID, ClassroomID     *string
	ValidFrom                  time.Time
	ValidTo, GeneratedUntil    *time.Time
	GroupType                  string
}

// Generate создаёт экземпляры занятий серии до даты until (включительно). Идемпотентна.
func Generate(ctx context.Context, tx pgx.Tx, scheduleID string, until time.Time) (int, error) {
	var s series
	var startSec, endSec int
	err := tx.QueryRow(ctx, `SELECT sc.id, sc.group_id, g.branch_id, b.timezone, sc.weekdays, EXTRACT(EPOCH FROM sc.start_time)::int, EXTRACT(EPOCH FROM sc.end_time)::int,
		COALESCE(sc.teacher_id, g.teacher_id), COALESCE(sc.classroom_id, g.classroom_id), sc.valid_from, sc.valid_to, sc.generated_until, g.type
		FROM schedules sc JOIN groups g ON g.id=sc.group_id JOIN branches b ON b.id=g.branch_id WHERE sc.id=$1 AND g.status='active'`, scheduleID).
		Scan(&s.ID, &s.GroupID, &s.BranchID, &s.TZ, &s.Weekdays, &startSec, &endSec, &s.TeacherID, &s.ClassroomID, &s.ValidFrom, &s.ValidTo, &s.GeneratedUntil, &s.GroupType)
	if err == pgx.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	loc, err := time.LoadLocation(s.TZ)
	if err != nil {
		loc, _ = time.LoadLocation("Asia/Almaty")
	}
	s.Start = time.Duration(startSec) * time.Second
	s.End = time.Duration(endSec) * time.Second

	var onHoliday string
	tx.QueryRow(ctx, `SELECT coalesce(settings->>'lesson_on_holiday','needs_decision') FROM tenants WHERE id = app_current_tenant()`).Scan(&onHoliday)

	from := s.ValidFrom
	if s.GeneratedUntil != nil && s.GeneratedUntil.After(from) {
		from = s.GeneratedUntil.AddDate(0, 0, 1)
	}
	if s.ValidTo != nil && until.After(*s.ValidTo) {
		until = *s.ValidTo
	}
	if from.After(until) {
		return 0, nil
	}
	// нерабочие дни: глобальные РК + организации (+ филиала)
	rows, err := tx.Query(ctx, `SELECT day, is_workday FROM calendar_holidays WHERE day BETWEEN $1 AND $2 AND (branch_id IS NULL OR branch_id=$3)`, from, until, s.BranchID)
	if err != nil {
		return 0, err
	}
	holidays := map[string]bool{}
	for rows.Next() {
		var d time.Time
		var workday bool
		rows.Scan(&d, &workday)
		if !workday {
			holidays[d.Format("2006-01-02")] = true
		}
	}
	rows.Close()

	lessonType := "regular"
	if s.GroupType == "individual" {
		lessonType = "individual"
	}
	created := 0
	for d := from; !d.After(until); d = d.AddDate(0, 0, 1) {
		if !slices.Contains(s.Weekdays, int32(isoWeekday(d))) {
			continue
		}
		status := "planned"
		if holidays[d.Format("2006-01-02")] {
			if onHoliday == "skip" {
				continue
			}
			status = "needs_decision"
		}
		day := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc)
		starts, ends := day.Add(s.Start), day.Add(s.End)
		tag, err := tx.Exec(ctx, `INSERT INTO lessons (tenant_id, branch_id, group_id, schedule_id, teacher_id, classroom_id, starts_at, ends_at, type, status)
			VALUES (app_current_tenant(), $1, $2, $3, $4, $5, $6, $7, $8, $9) ON CONFLICT (schedule_id, starts_at) WHERE schedule_id IS NOT NULL DO NOTHING`,
			s.BranchID, s.GroupID, s.ID, s.TeacherID, s.ClassroomID, starts.UTC(), ends.UTC(), lessonType, status)
		if err != nil {
			return created, err
		}
		created += int(tag.RowsAffected())
	}
	_, err = tx.Exec(ctx, `UPDATE schedules SET generated_until=$2 WHERE id=$1`, s.ID, until)
	return created, err
}

// ExtendAll — фоновая задача: продлевает все активные серии до горизонта.
func ExtendAll(ctx context.Context, tx pgx.Tx) (int, error) {
	rows, err := tx.Query(ctx, `SELECT id FROM schedules WHERE tenant_id = app_current_tenant() AND (valid_to IS NULL OR valid_to >= CURRENT_DATE)`)
	if err != nil {
		return 0, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return 0, err
	}
	total := 0
	until := time.Now().Add(Horizon)
	for _, id := range ids {
		n, err := Generate(ctx, tx, id, until)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

type Conflict struct {
	Kind      string    `json:"kind"` // teacher, classroom, group, capacity
	LessonID  string    `json:"lesson_id,omitempty"`
	GroupName string    `json:"group_name,omitempty"`
	StartsAt  time.Time `json:"starts_at,omitempty"`
	Message   string    `json:"message"`
}

// Conflicts проверяет пересечения занятия (ТЗ 11.4): преподаватель, кабинет (кроме онлайн), группа, вместимость кабинета.
func Conflicts(ctx context.Context, tx pgx.Tx, excludeID, groupID string, teacherID, classroomID *string, starts, ends time.Time) ([]Conflict, error) {
	out := []Conflict{}
	rows, err := tx.Query(ctx, `SELECT l.id, g.name, l.starts_at,
		CASE WHEN l.teacher_id = $3::uuid THEN 'teacher' WHEN l.classroom_id = $4::uuid AND NOT coalesce(cl.is_online,false) THEN 'classroom' ELSE 'group' END,
		coalesce(e.full_name,''), coalesce(cl.name,'')
		FROM lessons l JOIN groups g ON g.id=l.group_id LEFT JOIN classrooms cl ON cl.id=l.classroom_id LEFT JOIN employees e ON e.id=l.teacher_id
		WHERE l.status NOT IN ('cancelled') AND l.id::text <> $1 AND l.starts_at < $6 AND l.ends_at > $5
		AND (l.teacher_id = $3::uuid OR (l.classroom_id = $4::uuid AND NOT coalesce(cl.is_online,false)) OR l.group_id = $2::uuid)`,
		excludeID, groupID, teacherID, classroomID, starts, ends)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var c Conflict
		var teacher, room string
		rows.Scan(&c.LessonID, &c.GroupName, &c.StartsAt, &c.Kind, &teacher, &room)
		switch c.Kind {
		case "teacher":
			c.Message = teacher + " уже ведёт «" + c.GroupName + "» в это время"
		case "classroom":
			c.Message = room + " занят группой «" + c.GroupName + "»"
		default:
			c.Message = "У группы уже есть занятие в это время"
		}
		out = append(out, c)
	}
	rows.Close()
	if classroomID != nil {
		var capacity *int
		var online bool
		var size int
		tx.QueryRow(ctx, `SELECT capacity, is_online, (SELECT count(*) FROM group_students WHERE group_id=$2 AND left_at IS NULL) FROM classrooms WHERE id=$1`, *classroomID, groupID).
			Scan(&capacity, &online, &size)
		if !online && capacity != nil && size > *capacity {
			out = append(out, Conflict{Kind: "capacity", Message: "В группе больше учеников, чем мест в кабинете"})
		}
	}
	return out, nil
}

func blocking(cs []Conflict) bool {
	for _, c := range cs {
		if c.Kind != "capacity" {
			return true
		}
	}
	return false
}
