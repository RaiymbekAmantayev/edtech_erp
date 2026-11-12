package schedule

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"emp/internal/platform/auth"
	"emp/internal/platform/database"
	"emp/internal/platform/httpx"
	mw "emp/internal/platform/middleware"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

type Handler struct{ DB *database.DB }

func (h *Handler) Routes(r chi.Router) {
	// просмотр: администраторы — всё, преподаватель — свои занятия (scope=own)
	r.With(mw.Perm("schedule.view", "attendance.mark")).Get("/lessons", httpx.Handler(h.lessons).ServeHTTP)
	r.With(mw.Perm("schedule.view", "attendance.mark")).Get("/lessons/{id}", httpx.Handler(h.lesson).ServeHTTP)
	r.Group(func(r chi.Router) {
		r.Use(mw.Perm("schedule.edit"))
		r.Post("/schedules", httpx.Handler(h.createSeries).ServeHTTP)
		r.Post("/lessons", httpx.Handler(h.createLesson).ServeHTTP)
		r.Patch("/lessons/{id}", httpx.Handler(h.updateLesson).ServeHTTP)
		r.Post("/lessons/{id}/cancel", httpx.Handler(h.cancel).ServeHTTP)
		r.Post("/lessons/check", httpx.Handler(h.check).ServeHTTP)
		r.Get("/lessons/{id}/substitutes", httpx.Handler(h.substitutes).ServeHTTP)
		r.Post("/lessons/{id}/substitute", httpx.Handler(h.substitute).ServeHTTP)
	})
	r.With(mw.Perm("schedule.edit", "leads.edit")).Post("/lessons/{id}/guests", httpx.Handler(h.addGuest).ServeHTTP)
}

type LessonDTO struct {
	ID            string     `json:"id"`
	GroupID       string     `json:"group_id"`
	GroupName     string     `json:"group_name"`
	GroupType     string     `json:"group_type"`
	CourseName    string     `json:"course_name"`
	BranchID      string     `json:"branch_id"`
	TeacherID     *string    `json:"teacher_id"`
	TeacherName   *string    `json:"teacher_name"`
	ClassroomID   *string    `json:"classroom_id"`
	ClassroomName *string    `json:"classroom_name"`
	Capacity      *int       `json:"capacity"`
	StartsAt      time.Time  `json:"starts_at"`
	EndsAt        time.Time  `json:"ends_at"`
	Type          string     `json:"type"`
	Status        string     `json:"status"`
	State         string     `json:"state"` // done, needs_marking, in_progress, planned, cancelled, needs_decision, trial
	Topic         *string    `json:"topic"`
	MeetingURL    *string    `json:"meeting_url"`
	ScheduleID    *string    `json:"schedule_id"`
	Students      int        `json:"students"`
	Trials        int        `json:"trials"`
	Marked        int        `json:"marked"`
	Conflict      bool       `json:"conflict"`
	ClosedAt      *time.Time `json:"closed_at"`
	SubstituteFor *string    `json:"substitute_for"` // основной преподаватель группы, если урок ведёт замена
}

const lessonSel = `SELECT l.id, l.group_id, g.name, g.type, c.name, l.branch_id, l.teacher_id, e.full_name, l.classroom_id, cl.name, coalesce(g.capacity, cl.capacity),
	l.starts_at, l.ends_at, l.type, l.status, l.topic, l.meeting_url, l.schedule_id, l.closed_at,
	(SELECT count(*) FROM group_students gs WHERE gs.group_id=l.group_id AND gs.joined_at <= (l.starts_at AT TIME ZONE b.timezone)::date
		AND (gs.left_at IS NULL OR gs.left_at > (l.starts_at AT TIME ZONE b.timezone)::date)),
	(SELECT count(*) FROM lesson_guests lg WHERE lg.lesson_id=l.id AND lg.is_trial),
	(SELECT count(*) FROM attendance a WHERE a.lesson_id=l.id),
	EXISTS (SELECT 1 FROM lessons x LEFT JOIN classrooms xc ON xc.id=x.classroom_id WHERE x.id<>l.id AND x.status<>'cancelled' AND l.status<>'cancelled'
		AND x.starts_at < l.ends_at AND x.ends_at > l.starts_at
		AND (x.teacher_id=l.teacher_id OR (x.classroom_id=l.classroom_id AND NOT coalesce(xc.is_online,false)))),
	(SELECT sf.full_name FROM employees sf WHERE sf.id=l.substitute_for)
	FROM lessons l JOIN groups g ON g.id=l.group_id JOIN courses c ON c.id=g.course_id JOIN branches b ON b.id=l.branch_id
	LEFT JOIN employees e ON e.id=l.teacher_id LEFT JOIN classrooms cl ON cl.id=l.classroom_id`

// LessonSelect — общий SELECT занятий для других модулей (дашборд, кабинет преподавателя).
func LessonSelect() string { return lessonSel }

func ScanLessons(rows pgx.Rows) ([]LessonDTO, error) {
	defer rows.Close()
	out := []LessonDTO{}
	now := time.Now()
	for rows.Next() {
		var l LessonDTO
		if err := rows.Scan(&l.ID, &l.GroupID, &l.GroupName, &l.GroupType, &l.CourseName, &l.BranchID, &l.TeacherID, &l.TeacherName, &l.ClassroomID, &l.ClassroomName, &l.Capacity,
			&l.StartsAt, &l.EndsAt, &l.Type, &l.Status, &l.Topic, &l.MeetingURL, &l.ScheduleID, &l.ClosedAt, &l.Students, &l.Trials, &l.Marked, &l.Conflict, &l.SubstituteFor); err != nil {
			return nil, err
		}
		l.State = State(l.Status, l.StartsAt, l.EndsAt, l.Trials, now)
		out = append(out, l)
	}
	return out, rows.Err()
}

func State(status string, starts, ends time.Time, trials int, now time.Time) string {
	switch status {
	case "done", "cancelled", "needs_decision":
		return status
	}
	switch {
	case now.After(ends):
		return "needs_marking"
	case now.After(starts):
		return "in_progress"
	case trials > 0:
		return "trial"
	}
	return "planned"
}

// LessonFilter: преподаватель без права schedule.view видит только свои занятия.
func LessonFilter(p *auth.Principal, q map[string]string) (string, []any) {
	args := []any{}
	add := func(v any) string { args = append(args, v); return "$" + strconv.Itoa(len(args)) }
	conds := []string{}
	if !p.Can("schedule.view") {
		conds = append(conds, "l.teacher_id = "+add(p.EmployeeID)+"::uuid")
	}
	if bf := p.BranchFilter(); bf != nil && p.Can("schedule.view") {
		conds = append(conds, "l.branch_id = ANY("+add(bf)+"::uuid[])")
	}
	if v := q["from"]; v != "" {
		conds = append(conds, "l.starts_at >= "+add(v)+"::timestamptz")
	}
	if v := q["to"]; v != "" {
		conds = append(conds, "l.starts_at < "+add(v)+"::timestamptz")
	}
	for _, k := range []string{"branch_id", "group_id", "teacher_id", "classroom_id"} {
		if v := q[k]; v != "" {
			conds = append(conds, "l."+k+" = "+add(v)+"::uuid")
		}
	}
	if q["unmarked"] == "1" {
		conds = append(conds, "l.status='planned' AND l.ends_at < NOW()")
	}
	if q["hide_cancelled"] == "1" {
		conds = append(conds, "l.status <> 'cancelled'")
	}
	if len(conds) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

func (h *Handler) lessons(w http.ResponseWriter, r *http.Request) error {
	p := auth.FromContext(r.Context())
	q := map[string]string{}
	for k := range r.URL.Query() {
		q[k] = r.URL.Query().Get(k)
	}
	if !p.Can("schedule.view") && p.EmployeeID == "" {
		return httpx.OK(w, []LessonDTO{})
	}
	var out []LessonDTO
	err := h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		where, args := LessonFilter(p, q)
		limit := httpx.QueryInt(r, "limit", 500, 2000)
		rows, err := tx.Query(r.Context(), lessonSel+where+" ORDER BY l.starts_at, g.name LIMIT "+strconv.Itoa(limit), args...)
		if err != nil {
			return err
		}
		out, err = ScanLessons(rows)
		return err
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, out)
}

func (h *Handler) lesson(w http.ResponseWriter, r *http.Request) error {
	p := auth.FromContext(r.Context())
	var out []LessonDTO
	err := h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), lessonSel+" WHERE l.id=$1", chi.URLParam(r, "id"))
		if err != nil {
			return err
		}
		out, err = ScanLessons(rows)
		return err
	})
	if err != nil {
		return err
	}
	if len(out) == 0 || (!p.Can("schedule.view") && (out[0].TeacherID == nil || *out[0].TeacherID != p.EmployeeID)) {
		return httpx.NotFound("LESSON_NOT_FOUND")
	}
	return httpx.OK(w, out[0])
}

// parseLocal собирает момент времени из даты и времени в часовом поясе филиала.
func parseLocal(date, clock, tz string) (time.Time, error) {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc, _ = time.LoadLocation("Asia/Almaty")
	}
	return time.ParseInLocation("2006-01-02 15:04", date+" "+clock, loc)
}

func (h *Handler) createSeries(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		GroupID     string  `json:"group_id"`
		Weekdays    []int   `json:"weekdays"`
		StartTime   string  `json:"start_time"`
		EndTime     string  `json:"end_time"`
		TeacherID   *string `json:"teacher_id"`
		ClassroomID *string `json:"classroom_id"`
		ValidFrom   string  `json:"valid_from"`
		ValidTo     string  `json:"valid_to"`
		Force       bool    `json:"force"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	v := &httpx.Validation{}
	if req.GroupID == "" {
		v.Add("group_id", "REQUIRED")
	}
	if len(req.Weekdays) == 0 {
		v.Add("weekdays", "REQUIRED")
	}
	for _, d := range req.Weekdays {
		if d < 1 || d > 7 {
			v.Add("weekdays", "INVALID")
		}
	}
	st, err1 := time.Parse("15:04", req.StartTime)
	et, err2 := time.Parse("15:04", req.EndTime)
	if err1 != nil || err2 != nil || !et.After(st) {
		v.Add("end_time", "INVALID_TIME")
	}
	if req.ValidFrom == "" {
		req.ValidFrom = time.Now().Format("2006-01-02")
	}
	if err := v.Err(); err != nil {
		return err
	}
	ctx := r.Context()
	var id string
	var created int
	var conflicts []Conflict
	err := h.DB.Tx(ctx, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO schedules (tenant_id, group_id, weekdays, start_time, end_time, teacher_id, classroom_id, valid_from, valid_to)
			VALUES (app_current_tenant(), $1, $2, $3::time, $4::time, NULLIF($5,'')::uuid, NULLIF($6,'')::uuid, $7::date, NULLIF($8,'')::date) RETURNING id`,
			req.GroupID, req.Weekdays, req.StartTime, req.EndTime, deref(req.TeacherID), deref(req.ClassroomID), req.ValidFrom, req.ValidTo).Scan(&id); err != nil {
			return err
		}
		var err error
		if created, err = Generate(ctx, tx, id, time.Now().Add(Horizon)); err != nil {
			return err
		}
		// проверка конфликтов по всем сгенерированным занятиям серии
		rows, err := tx.Query(ctx, `SELECT id, group_id, teacher_id::text, classroom_id::text, starts_at, ends_at FROM lessons WHERE schedule_id=$1 AND status <> 'cancelled'`, id)
		if err != nil {
			return err
		}
		type lr struct {
			id, group      string
			teacher, room  *string
			starts, ends   time.Time
		}
		var ls []lr
		for rows.Next() {
			var x lr
			rows.Scan(&x.id, &x.group, &x.teacher, &x.room, &x.starts, &x.ends)
			ls = append(ls, x)
		}
		rows.Close()
		for _, x := range ls {
			cs, err := Conflicts(ctx, tx, x.id, x.group, x.teacher, x.room, x.starts, x.ends)
			if err != nil {
				return err
			}
			conflicts = append(conflicts, cs...)
			if len(conflicts) > 20 {
				break
			}
		}
		if blocking(conflicts) && !req.Force {
			return &httpx.Error{Status: 409, Code: "SCHEDULE_CONFLICT", Message: conflicts[0].Message}
		}
		return database.Audit(ctx, tx, "schedule", id, "create", nil, req)
	})
	if err != nil {
		if he, ok := err.(*httpx.Error); ok && he.Code == "SCHEDULE_CONFLICT" {
			return httpx.OKMeta(w, nil, map[string]any{"conflicts": conflicts, "requires_force": true})
		}
		return err
	}
	return httpx.Created(w, map[string]any{"id": id, "lessons_created": created, "conflicts": conflicts})
}

func (h *Handler) branchTZ(r *http.Request, tx pgx.Tx, groupID string) (string, string, error) {
	var branch, tz string
	err := tx.QueryRow(r.Context(), `SELECT g.branch_id, b.timezone FROM groups g JOIN branches b ON b.id=g.branch_id WHERE g.id=$1`, groupID).Scan(&branch, &tz)
	return branch, tz, err
}

type lessonReq struct {
	GroupID     string  `json:"group_id"`
	Date        string  `json:"date"`
	StartTime   string  `json:"start_time"`
	EndTime     string  `json:"end_time"`
	TeacherID   *string `json:"teacher_id"`
	ClassroomID *string `json:"classroom_id"`
	Type        string  `json:"type"`
	Topic       *string `json:"topic"`
	MeetingURL  *string `json:"meeting_url"`
	Status      *string `json:"status"`
	Scope       string  `json:"scope"` // this | following | all
	Force       bool    `json:"force"`
}

func (h *Handler) check(w http.ResponseWriter, r *http.Request) error {
	var req lessonReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	var cs []Conflict
	err := h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		_, tz, err := h.branchTZ(r, tx, req.GroupID)
		if err != nil {
			return err
		}
		s, err1 := parseLocal(req.Date, req.StartTime, tz)
		e, err2 := parseLocal(req.Date, req.EndTime, tz)
		if err1 != nil || err2 != nil {
			return httpx.BadRequest("INVALID_TIME", "Неверное время")
		}
		cs, err = Conflicts(r.Context(), tx, r.URL.Query().Get("exclude"), req.GroupID, req.TeacherID, req.ClassroomID, s, e)
		return err
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, cs)
}

func (h *Handler) createLesson(w http.ResponseWriter, r *http.Request) error {
	var req lessonReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	ctx := r.Context()
	var id string
	var conflicts []Conflict
	err := h.DB.Tx(ctx, func(tx pgx.Tx) error {
		branch, tz, err := h.branchTZ(r, tx, req.GroupID)
		if err != nil {
			return httpx.BadRequest("GROUP_NOT_FOUND", "Группа не найдена")
		}
		s, err1 := parseLocal(req.Date, req.StartTime, tz)
		e, err2 := parseLocal(req.Date, req.EndTime, tz)
		if err1 != nil || err2 != nil || !e.After(s) {
			return (&httpx.Validation{Fields: []httpx.FieldError{{Field: "end_time", Code: "INVALID_TIME"}}}).Err()
		}
		if req.TeacherID == nil || req.ClassroomID == nil {
			var t, c *string
			tx.QueryRow(ctx, `SELECT teacher_id::text, classroom_id::text FROM groups WHERE id=$1`, req.GroupID).Scan(&t, &c)
			if req.TeacherID == nil {
				req.TeacherID = t
			}
			if req.ClassroomID == nil {
				req.ClassroomID = c
			}
		}
		if conflicts, err = Conflicts(ctx, tx, "", req.GroupID, nilIfEmpty(req.TeacherID), nilIfEmpty(req.ClassroomID), s, e); err != nil {
			return err
		}
		if blocking(conflicts) && !req.Force {
			return &httpx.Error{Status: 409, Code: "SCHEDULE_CONFLICT", Message: conflicts[0].Message}
		}
		typ := req.Type
		if !slices.Contains([]string{"regular", "trial", "makeup", "individual", "online"}, typ) {
			typ = "regular"
		}
		if err := tx.QueryRow(ctx, `INSERT INTO lessons (tenant_id, branch_id, group_id, teacher_id, classroom_id, starts_at, ends_at, type, topic, meeting_url)
			VALUES (app_current_tenant(), $1, $2, NULLIF($3,'')::uuid, NULLIF($4,'')::uuid, $5, $6, $7, $8, $9) RETURNING id`,
			branch, req.GroupID, deref(req.TeacherID), deref(req.ClassroomID), s.UTC(), e.UTC(), typ, req.Topic, req.MeetingURL).Scan(&id); err != nil {
			return err
		}
		if err := database.Publish(ctx, tx, "lesson.created", map[string]any{"lesson_id": id}); err != nil {
			return err
		}
		return database.Audit(ctx, tx, "lesson", id, "create", nil, req)
	})
	if err != nil {
		if he, ok := err.(*httpx.Error); ok && he.Code == "SCHEDULE_CONFLICT" {
			return httpx.OKMeta(w, nil, map[string]any{"conflicts": conflicts, "requires_force": true})
		}
		return err
	}
	return httpx.Created(w, map[string]any{"id": id, "conflicts": conflicts})
}

// updateLesson: перенос/замена. Scope: «Только это занятие» / «Это и все последующие» / «Вся серия» (ТЗ 11.5).
// Проведённые занятия при изменении серии не меняются.
func (h *Handler) updateLesson(w http.ResponseWriter, r *http.Request) error {
	var req lessonReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	var conflicts []Conflict
	err := h.DB.Tx(ctx, func(tx pgx.Tx) error {
		var groupID, tz, status string
		var scheduleID, teacherID, classroomID *string
		var starts, ends time.Time
		if err := tx.QueryRow(ctx, `SELECT l.group_id, b.timezone, l.status, l.schedule_id::text, l.teacher_id::text, l.classroom_id::text, l.starts_at, l.ends_at
			FROM lessons l JOIN branches b ON b.id=l.branch_id WHERE l.id=$1 FOR UPDATE`, id).
			Scan(&groupID, &tz, &status, &scheduleID, &teacherID, &classroomID, &starts, &ends); err != nil {
			return err
		}
		if status == "done" && req.Scope != "following" && req.Scope != "all" && (req.Date != "" || req.StartTime != "") {
			return httpx.Conflict("LESSON_CLOSED", "Проведённое занятие нельзя перенести")
		}
		loc, _ := time.LoadLocation(tz)
		newStart, newEnd := starts, ends
		if req.Date != "" || req.StartTime != "" || req.EndTime != "" {
			date := firstNonEmpty(req.Date, starts.In(loc).Format("2006-01-02"))
			s, err1 := parseLocal(date, firstNonEmpty(req.StartTime, starts.In(loc).Format("15:04")), tz)
			e, err2 := parseLocal(date, firstNonEmpty(req.EndTime, ends.In(loc).Format("15:04")), tz)
			if err1 != nil || err2 != nil || !e.After(s) {
				return (&httpx.Validation{Fields: []httpx.FieldError{{Field: "end_time", Code: "INVALID_TIME"}}}).Err()
			}
			newStart, newEnd = s, e
		}
		if req.TeacherID != nil {
			teacherID = nilIfEmpty(req.TeacherID)
		}
		if req.ClassroomID != nil {
			classroomID = nilIfEmpty(req.ClassroomID)
		}
		var err error
		if conflicts, err = Conflicts(ctx, tx, id, groupID, teacherID, classroomID, newStart, newEnd); err != nil {
			return err
		}
		if blocking(conflicts) && !req.Force {
			return &httpx.Error{Status: 409, Code: "SCHEDULE_CONFLICT", Message: conflicts[0].Message}
		}
		scope := req.Scope
		if scope == "" || scheduleID == nil {
			scope = "this"
		}
		switch scope {
		case "this":
			newStatus := status
			if req.Status != nil {
				newStatus = *req.Status
			} else if status == "needs_decision" {
				newStatus = "planned" // решение по праздничному дню принято
			}
			if _, err := tx.Exec(ctx, `UPDATE lessons SET starts_at=$2, ends_at=$3, teacher_id=$4::uuid, classroom_id=$5::uuid, topic=COALESCE($6, topic),
				meeting_url=COALESCE($7, meeting_url), status=$8, is_detached=(schedule_id IS NOT NULL) WHERE id=$1`,
				id, newStart.UTC(), newEnd.UTC(), teacherID, classroomID, req.Topic, req.MeetingURL, newStatus); err != nil {
				return err
			}
			if req.TeacherID != nil {
				// замена отмечается относительно основного преподавателя группы, начисление переходит новому
				if err := SetTeacher(ctx, tx, id, teacherID); err != nil {
					return err
				}
			}
			if !newStart.Equal(starts) {
				database.Publish(ctx, tx, "lesson.rescheduled", map[string]any{"lesson_id": id, "from": starts, "to": newStart})
			}
		case "following", "all":
			// серия: меняем правило и пересоздаём будущие незакрытые и не изменённые вручную занятия
			fromDate := starts.In(loc).Format("2006-01-02")
			if scope == "all" {
				fromDate = time.Now().In(loc).Format("2006-01-02")
			}
			var weekdays []int32
			var validTo *time.Time
			if err := tx.QueryRow(ctx, `SELECT weekdays, valid_to FROM schedules WHERE id=$1`, *scheduleID).Scan(&weekdays, &validTo); err != nil {
				return err
			}
			// день недели переносимого занятия заменяется новым
			oldWD, newWD := int32(isoWeekday(starts.In(loc))), int32(isoWeekday(newStart.In(loc)))
			if oldWD != newWD {
				for i := range weekdays {
					if weekdays[i] == oldWD {
						weekdays[i] = newWD
					}
				}
			}
			if _, err := tx.Exec(ctx, `UPDATE schedules SET valid_to=($2::date - 1) WHERE id=$1`, *scheduleID, fromDate); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `DELETE FROM lessons l USING branches b WHERE b.id=l.branch_id AND l.schedule_id=$1 AND l.status IN ('planned','needs_decision')
				AND NOT l.is_detached AND (l.starts_at AT TIME ZONE b.timezone)::date >= $2::date
				AND NOT EXISTS (SELECT 1 FROM attendance a WHERE a.lesson_id=l.id) AND NOT EXISTS (SELECT 1 FROM subscription_usages u WHERE u.lesson_id=l.id)
				AND NOT EXISTS (SELECT 1 FROM lesson_guests g WHERE g.lesson_id=l.id)`, *scheduleID, fromDate); err != nil {
				return err
			}
			var newID string
			if err := tx.QueryRow(ctx, `INSERT INTO schedules (tenant_id, group_id, weekdays, start_time, end_time, teacher_id, classroom_id, valid_from, valid_to)
				VALUES (app_current_tenant(), $1, $2, $3::time, $4::time, $5::uuid, $6::uuid, $7::date, $8) RETURNING id`,
				groupID, weekdays, newStart.In(loc).Format("15:04"), newEnd.In(loc).Format("15:04"), teacherID, classroomID, fromDate, validTo).Scan(&newID); err != nil {
				return err
			}
			if _, err := Generate(ctx, tx, newID, time.Now().Add(Horizon)); err != nil {
				return err
			}
			database.Publish(ctx, tx, "schedule.changed", map[string]any{"group_id": groupID, "schedule_id": newID})
		}
		return database.Audit(ctx, tx, "lesson", id, "update", map[string]any{"starts_at": starts, "teacher_id": teacherID}, req)
	})
	if err != nil {
		if he, ok := err.(*httpx.Error); ok && he.Code == "SCHEDULE_CONFLICT" {
			return httpx.OKMeta(w, nil, map[string]any{"conflicts": conflicts, "requires_force": true})
		}
		return err
	}
	return httpx.OK(w, map[string]any{"id": id, "conflicts": conflicts})
}

func (h *Handler) cancel(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Reason string `json:"reason"`
		Scope  string `json:"scope"`
	}
	httpx.Decode(r, &req)
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	return h.DB.Tx(ctx, func(tx pgx.Tx) error {
		var status string
		var scheduleID *string
		var starts time.Time
		if err := tx.QueryRow(ctx, `SELECT status, schedule_id::text, starts_at FROM lessons WHERE id=$1`, id).Scan(&status, &scheduleID, &starts); err != nil {
			return err
		}
		if status == "done" {
			return httpx.Conflict("LESSON_CLOSED", "Проведённое занятие нельзя отменить")
		}
		if _, err := tx.Exec(ctx, `UPDATE lessons SET status='cancelled', topic=COALESCE(NULLIF($2,''), topic) WHERE id=$1`, id, req.Reason); err != nil {
			return err
		}
		if req.Scope == "following" && scheduleID != nil {
			// «это и все последующие»: закрываем серию и отменяем её будущие занятия
			if _, err := tx.Exec(ctx, `UPDATE lessons SET status='cancelled' WHERE schedule_id=$1 AND starts_at >= $2 AND status IN ('planned','needs_decision')`, *scheduleID, starts); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE schedules SET valid_to=($2::timestamptz)::date - 1 WHERE id=$1`, *scheduleID, starts); err != nil {
				return err
			}
		}
		if err := database.Publish(ctx, tx, "lesson.cancelled", map[string]any{"lesson_id": id, "reason": req.Reason}); err != nil {
			return err
		}
		return database.Audit(ctx, tx, "lesson", id, "cancel", nil, req)
	})
}

// addGuest — пробный ученик/лид на занятии (отображается в расписании и посещаемости с пометкой «Пробный»).
func (h *Handler) addGuest(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		LeadID    string `json:"lead_id"`
		StudentID string `json:"student_id"`
		IsTrial   *bool  `json:"is_trial"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	trial := req.IsTrial == nil || *req.IsTrial
	id := chi.URLParam(r, "id")
	return h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		ctx := r.Context()
		if _, err := tx.Exec(ctx, `INSERT INTO lesson_guests (tenant_id, lesson_id, student_id, lead_id, is_trial) VALUES (app_current_tenant(), $1, NULLIF($2,'')::uuid, NULLIF($3,'')::uuid, $4)`,
			id, req.StudentID, req.LeadID, trial); err != nil {
			return err
		}
		if req.LeadID != "" {
			tx.Exec(ctx, `UPDATE leads SET stage='trial_scheduled', updated_at=NOW() WHERE id=$1 AND stage IN ('new','contacted')`, req.LeadID)
		}
		return database.Audit(ctx, tx, "lesson", id, "guest_added", nil, req)
	})
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func nilIfEmpty(s *string) *string {
	if s == nil || *s == "" {
		return nil
	}
	return s
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
