// Пакет group — курсы, учебные группы, индивидуальный формат и лист ожидания.
package group

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"emp/internal/modules/customfield"
	"emp/internal/platform/auth"
	"emp/internal/platform/database"
	"emp/internal/platform/httpx"
	mw "emp/internal/platform/middleware"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

type Handler struct{ DB *database.DB }

func (h *Handler) Routes(r chi.Router) {
	r.With(mw.Perm("groups.view", "schedule.view", "leads.view", "students.view")).Get("/courses", httpx.Handler(h.courses).ServeHTTP)
	r.With(mw.Perm("groups.view", "schedule.view", "leads.view", "students.view")).Get("/groups", httpx.Handler(h.list).ServeHTTP)
	r.With(mw.Perm("groups.view")).Get("/groups/{id}", httpx.Handler(h.get).ServeHTTP)
	r.Group(func(r chi.Router) {
		r.Use(mw.Perm("groups.edit"))
		r.Post("/courses", httpx.Handler(h.createCourse).ServeHTTP)
		r.Patch("/courses/{id}", httpx.Handler(h.updateCourse).ServeHTTP)
		r.Post("/groups", httpx.Handler(h.create).ServeHTTP)
		r.Patch("/groups/{id}", httpx.Handler(h.update).ServeHTTP)
		r.Post("/groups/{id}/waitlist", httpx.Handler(h.addWaitlist).ServeHTTP)
		r.Delete("/groups/{id}/waitlist/{wid}", httpx.Handler(h.removeWaitlist).ServeHTTP)
	})
}

type course struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	Description     *string `json:"description"`
	Category        *string `json:"category"`
	DurationMinutes int     `json:"duration_minutes"`
	LessonsCount    int     `json:"lessons_count"`
	PriceTiyn       int64   `json:"price_tiyn"`
	Status          string  `json:"status"`
	Groups          int     `json:"groups"`
	Students        int     `json:"students"`
}

func (h *Handler) courses(w http.ResponseWriter, r *http.Request) error {
	out := []course{}
	err := h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `SELECT c.id, c.name, c.description, c.category, c.duration_minutes, c.lessons_count, c.price_tiyn, c.status,
			(SELECT count(*) FROM groups g WHERE g.course_id=c.id AND g.status='active' AND g.type='group'),
			(SELECT count(DISTINCT gs.student_id) FROM groups g JOIN group_students gs ON gs.group_id=g.id AND gs.left_at IS NULL WHERE g.course_id=c.id)
			FROM courses c WHERE ($1 OR c.status='active') ORDER BY c.name`, r.URL.Query().Get("all") == "1")
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c course
			rows.Scan(&c.ID, &c.Name, &c.Description, &c.Category, &c.DurationMinutes, &c.LessonsCount, &c.PriceTiyn, &c.Status, &c.Groups, &c.Students)
			out = append(out, c)
		}
		return rows.Err()
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, out)
}

func (h *Handler) createCourse(w http.ResponseWriter, r *http.Request) error {
	var c course
	if err := httpx.Decode(r, &c); err != nil {
		return err
	}
	v := &httpx.Validation{}
	if strings.TrimSpace(c.Name) == "" {
		v.Add("name", "REQUIRED")
	}
	if c.PriceTiyn < 0 {
		v.Add("price_tiyn", "INVALID")
	}
	if err := v.Err(); err != nil {
		return err
	}
	if c.DurationMinutes <= 0 {
		c.DurationMinutes = 90
	}
	if c.LessonsCount <= 0 {
		c.LessonsCount = 12
	}
	var id string
	err := h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		if err := tx.QueryRow(r.Context(), `INSERT INTO courses (tenant_id, name, description, category, duration_minutes, lessons_count, price_tiyn)
			VALUES (app_current_tenant(), $1, $2, $3, $4, $5, $6) RETURNING id`,
			strings.TrimSpace(c.Name), c.Description, c.Category, c.DurationMinutes, c.LessonsCount, c.PriceTiyn).Scan(&id); err != nil {
			return err
		}
		return database.Audit(r.Context(), tx, "course", id, "create", nil, c)
	})
	if err != nil {
		return err
	}
	return httpx.Created(w, map[string]string{"id": id})
}

func (h *Handler) updateCourse(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Name            *string `json:"name"`
		Description     *string `json:"description"`
		Category        *string `json:"category"`
		DurationMinutes *int    `json:"duration_minutes"`
		LessonsCount    *int    `json:"lessons_count"`
		PriceTiyn       *int64  `json:"price_tiyn"`
		Status          *string `json:"status"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	id := chi.URLParam(r, "id")
	return h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		ctx := r.Context()
		var oldPrice int64
		tx.QueryRow(ctx, `SELECT price_tiyn FROM courses WHERE id=$1`, id).Scan(&oldPrice)
		_, err := tx.Exec(ctx, `UPDATE courses SET name=COALESCE($2,name), description=COALESCE($3,description), category=COALESCE($4,category),
			duration_minutes=COALESCE($5,duration_minutes), lessons_count=COALESCE($6,lessons_count), price_tiyn=COALESCE($7,price_tiyn), status=COALESCE($8,status) WHERE id=$1`,
			id, req.Name, req.Description, req.Category, req.DurationMinutes, req.LessonsCount, req.PriceTiyn, req.Status)
		if err != nil {
			return err
		}
		return database.Audit(ctx, tx, "course", id, "update", map[string]any{"price_tiyn": oldPrice}, req)
	})
}

type groupDTO struct {
	ID          string          `json:"id"`
	BranchID    string          `json:"branch_id"`
	BranchName  string          `json:"branch_name"`
	CourseID    string          `json:"course_id"`
	CourseName  string          `json:"course_name"`
	Name        string          `json:"name"`
	Type        string          `json:"type"`
	TeacherID   *string         `json:"teacher_id"`
	TeacherName *string         `json:"teacher_name"`
	ClassroomID *string         `json:"classroom_id"`
	Classroom   *string         `json:"classroom_name"`
	StartDate   *time.Time      `json:"start_date"`
	EndDate     *time.Time      `json:"end_date"`
	Capacity    *int            `json:"capacity"`
	Status      string          `json:"status"`
	Students    int             `json:"students"`
	Waitlist    int             `json:"waitlist"`
	Schedule    json.RawMessage `json:"schedule"`
	CF          map[string]any  `json:"custom_fields"`
}

const groupSel = `SELECT g.id, g.branch_id, b.name, g.course_id, c.name, g.name, g.type, g.teacher_id, e.full_name, g.classroom_id, cl.name, g.start_date, g.end_date, g.capacity, g.status,
	(SELECT count(*) FROM group_students gs WHERE gs.group_id=g.id AND gs.left_at IS NULL),
	(SELECT count(*) FROM group_waitlist wl WHERE wl.group_id=g.id),
	(SELECT coalesce(json_agg(json_build_object('id', sc.id, 'weekdays', sc.weekdays, 'start_time', to_char(sc.start_time,'HH24:MI'), 'end_time', to_char(sc.end_time,'HH24:MI'),
		'valid_from', sc.valid_from, 'valid_to', sc.valid_to) ORDER BY sc.valid_from), '[]') FROM schedules sc WHERE sc.group_id=g.id AND (sc.valid_to IS NULL OR sc.valid_to >= CURRENT_DATE)),
	g.custom_fields
	FROM groups g JOIN branches b ON b.id=g.branch_id JOIN courses c ON c.id=g.course_id
	LEFT JOIN employees e ON e.id=g.teacher_id LEFT JOIN classrooms cl ON cl.id=g.classroom_id`

func scanGroups(rows pgx.Rows) ([]groupDTO, error) {
	defer rows.Close()
	out := []groupDTO{}
	for rows.Next() {
		var g groupDTO
		if err := rows.Scan(&g.ID, &g.BranchID, &g.BranchName, &g.CourseID, &g.CourseName, &g.Name, &g.Type, &g.TeacherID, &g.TeacherName, &g.ClassroomID, &g.Classroom,
			&g.StartDate, &g.EndDate, &g.Capacity, &g.Status, &g.Students, &g.Waitlist, &g.Schedule, &g.CF); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) error {
	p := auth.FromContext(r.Context())
	q := r.URL.Query()
	var out []groupDTO
	err := h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		args := []any{}
		add := func(v any) string { args = append(args, v); return "$" + strconv.Itoa(len(args)) }
		conds := []string{}
		if q.Get("status") != "all" {
			conds = append(conds, "g.status='active'")
		}
		// индивидуальный формат скрыт из списка групп, если не запрошен явно
		switch q.Get("type") {
		case "individual":
			conds = append(conds, "g.type='individual'")
		case "all":
		default:
			conds = append(conds, "g.type='group'")
		}
		if b := q.Get("branch_id"); b != "" {
			conds = append(conds, "g.branch_id="+add(b)+"::uuid")
		}
		if bf := p.BranchFilter(); bf != nil {
			conds = append(conds, "g.branch_id = ANY("+add(bf)+"::uuid[])")
		}
		if c := q.Get("course_id"); c != "" {
			conds = append(conds, "g.course_id="+add(c)+"::uuid")
		}
		if t := q.Get("teacher_id"); t != "" {
			conds = append(conds, "g.teacher_id="+add(t)+"::uuid")
		}
		if s := q.Get("q"); s != "" {
			conds = append(conds, "(g.name ILIKE '%' || "+add(s)+" || '%' OR c.name ILIKE '%' || $"+strconv.Itoa(len(args))+" || '%')")
		}
		where := ""
		if len(conds) > 0 {
			where = " WHERE " + strings.Join(conds, " AND ")
		}
		rows, err := tx.Query(r.Context(), groupSel+where+" ORDER BY c.name, g.name", args...)
		if err != nil {
			return err
		}
		out, err = scanGroups(rows)
		return err
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, out)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	p := auth.FromContext(r.Context())
	out := map[string]any{}
	err := h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		ctx := r.Context()
		rows, err := tx.Query(ctx, groupSel+" WHERE g.id=$1", id)
		if err != nil {
			return err
		}
		list, err := scanGroups(rows)
		if err != nil {
			return err
		}
		if len(list) == 0 || !p.CanBranch(list[0].BranchID) {
			return httpx.NotFound("GROUP_NOT_FOUND")
		}
		fields, _ := customfield.Load(ctx, tx, "group", false)
		list[0].CF = customfield.Strip(fields, list[0].CF, p, false)
		out["group"] = list[0]
		out["fields"] = customfield.Visible(fields, p)
		srows, err := tx.Query(ctx, `SELECT s.id, s.full_name, s.phone, s.status, gs.joined_at, coalesce(b.balance_tiyn,0),
			(SELECT round(100.0*count(*) FILTER (WHERE a.status IN ('present','late'))/NULLIF(count(*),0)) FROM attendance a JOIN lessons l ON l.id=a.lesson_id WHERE a.student_id=s.id AND l.group_id=$1)
			FROM group_students gs JOIN students s ON s.id=gs.student_id LEFT JOIN student_balances b ON b.student_id=s.id
			WHERE gs.group_id=$1 AND gs.left_at IS NULL ORDER BY s.full_name`, id)
		if err != nil {
			return err
		}
		students := []map[string]any{}
		for srows.Next() {
			var sid, name, status string
			var phone *string
			var joined time.Time
			var bal int64
			var rate *float64
			srows.Scan(&sid, &name, &phone, &status, &joined, &bal, &rate)
			m := map[string]any{"id": sid, "full_name": name, "phone": phone, "status": status, "joined_at": joined, "attendance_rate": rate}
			if p.Can("finance.view") {
				m["balance_tiyn"] = bal
			}
			students = append(students, m)
		}
		srows.Close()
		out["students"] = students
		hrows, err := tx.Query(ctx, `SELECT s.id, s.full_name, gs.joined_at, gs.left_at, gs.leave_reason FROM group_students gs JOIN students s ON s.id=gs.student_id
			WHERE gs.group_id=$1 AND gs.left_at IS NOT NULL ORDER BY gs.left_at DESC LIMIT 50`, id)
		if err != nil {
			return err
		}
		left := []map[string]any{}
		for hrows.Next() {
			var sid, name string
			var joined, leftAt time.Time
			var reason *string
			hrows.Scan(&sid, &name, &joined, &leftAt, &reason)
			left = append(left, map[string]any{"id": sid, "full_name": name, "joined_at": joined, "left_at": leftAt, "reason": reason})
		}
		hrows.Close()
		out["left"] = left
		wrows, err := tx.Query(ctx, `SELECT w.id, w.priority, w.created_at, coalesce(s.full_name, l.full_name), CASE WHEN w.student_id IS NOT NULL THEN 'student' ELSE 'lead' END,
			coalesce(s.phone, l.phone) FROM group_waitlist w LEFT JOIN students s ON s.id=w.student_id LEFT JOIN leads l ON l.id=w.lead_id
			WHERE w.group_id=$1 ORDER BY w.priority DESC, w.created_at`, id)
		if err != nil {
			return err
		}
		wl := []map[string]any{}
		for wrows.Next() {
			var wid, name, kind string
			var prio int
			var at time.Time
			var phone *string
			wrows.Scan(&wid, &prio, &at, &name, &kind, &phone)
			wl = append(wl, map[string]any{"id": wid, "priority": prio, "created_at": at, "full_name": name, "kind": kind, "phone": phone})
		}
		wrows.Close()
		out["waitlist"] = wl
		return nil
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, out)
}

type groupReq struct {
	BranchID    *string        `json:"branch_id"`
	CourseID    *string        `json:"course_id"`
	Name        *string        `json:"name"`
	Type        *string        `json:"type"`
	TeacherID   *string        `json:"teacher_id"`
	ClassroomID *string        `json:"classroom_id"`
	StartDate   *string        `json:"start_date"`
	EndDate     *string        `json:"end_date"`
	Capacity    *int           `json:"capacity"`
	Status      *string        `json:"status"`
	CF          map[string]any `json:"custom_fields"`
	StudentID   string         `json:"student_id"` // для индивидуального формата
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) error {
	var req groupReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	p := auth.FromContext(r.Context())
	v := &httpx.Validation{}
	if req.BranchID == nil || *req.BranchID == "" {
		v.Add("branch_id", "REQUIRED")
	}
	if req.CourseID == nil || *req.CourseID == "" {
		v.Add("course_id", "REQUIRED")
	}
	typ := "group"
	if req.Type != nil && *req.Type == "individual" {
		typ = "individual"
	}
	if typ == "group" && (req.Name == nil || strings.TrimSpace(*req.Name) == "") {
		v.Add("name", "REQUIRED")
	}
	if typ == "individual" && req.StudentID == "" {
		v.Add("student_id", "REQUIRED")
	}
	if err := v.Err(); err != nil {
		return err
	}
	if !p.CanBranch(*req.BranchID) {
		return httpx.Forbidden()
	}
	ctx := r.Context()
	var id string
	err := h.DB.Tx(ctx, func(tx pgx.Tx) error {
		name := ""
		if req.Name != nil {
			name = strings.TrimSpace(*req.Name)
		}
		if typ == "individual" {
			// технически — группа с типом individual, скрытая из списка групп (ТЗ 10.5)
			var sname string
			if err := tx.QueryRow(ctx, `SELECT full_name FROM students WHERE id=$1`, req.StudentID).Scan(&sname); err != nil {
				return httpx.BadRequest("STUDENT_NOT_FOUND", "Ученик не найден")
			}
			if name == "" {
				parts := strings.Fields(sname)
				name = "Индив. · " + parts[0]
				if len(parts) > 1 {
					name += " " + string([]rune(parts[1])[:1]) + "."
				}
			}
			one := 1
			req.Capacity = &one
		}
		fields, err := customfield.Load(ctx, tx, "group", false)
		if err != nil {
			return err
		}
		cf, err := customfield.Apply(ctx, tx, fields, nil, req.CF, p, customfield.Context{Table: "groups", BranchID: *req.BranchID, Creating: true})
		if err != nil {
			return err
		}
		b, _ := json.Marshal(cf)
		if err := tx.QueryRow(ctx, `INSERT INTO groups (tenant_id, branch_id, course_id, name, type, teacher_id, classroom_id, start_date, end_date, capacity, custom_fields)
			VALUES (app_current_tenant(), $1, $2, $3, $4, NULLIF($5,'')::uuid, NULLIF($6,'')::uuid, NULLIF($7,'')::date, NULLIF($8,'')::date, $9, $10) RETURNING id`,
			*req.BranchID, *req.CourseID, name, typ, deref(req.TeacherID), deref(req.ClassroomID), deref(req.StartDate), deref(req.EndDate), req.Capacity, b).Scan(&id); err != nil {
			return err
		}
		if typ == "individual" {
			if _, err := tx.Exec(ctx, `INSERT INTO group_students (tenant_id, group_id, student_id) VALUES (app_current_tenant(), $1, $2)`, id, req.StudentID); err != nil {
				return err
			}
		}
		return database.Audit(ctx, tx, "group", id, "create", nil, req)
	})
	if err != nil {
		return err
	}
	return httpx.Created(w, map[string]string{"id": id})
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) error {
	var req groupReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	id := chi.URLParam(r, "id")
	p := auth.FromContext(r.Context())
	ctx := r.Context()
	return h.DB.Tx(ctx, func(tx pgx.Tx) error {
		var cur map[string]any
		var oldTeacher *string
		if err := tx.QueryRow(ctx, `SELECT custom_fields, teacher_id::text FROM groups WHERE id=$1 FOR UPDATE`, id).Scan(&cur, &oldTeacher); err != nil {
			return err
		}
		if req.CF != nil {
			fields, err := customfield.Load(ctx, tx, "group", false)
			if err != nil {
				return err
			}
			cf, err := customfield.Apply(ctx, tx, fields, cur, req.CF, p, customfield.Context{Table: "groups", RecordID: id})
			if err != nil {
				return err
			}
			b, _ := json.Marshal(cf)
			tx.Exec(ctx, `UPDATE groups SET custom_fields=$2 WHERE id=$1`, id, b)
		}
		if _, err := tx.Exec(ctx, `UPDATE groups SET name=COALESCE($2,name), teacher_id=CASE WHEN $3::text IS NULL THEN teacher_id ELSE NULLIF($3,'')::uuid END,
			classroom_id=CASE WHEN $4::text IS NULL THEN classroom_id ELSE NULLIF($4,'')::uuid END,
			start_date=CASE WHEN $5::text IS NULL THEN start_date ELSE NULLIF($5,'')::date END, end_date=CASE WHEN $6::text IS NULL THEN end_date ELSE NULLIF($6,'')::date END,
			capacity=COALESCE($7,capacity), status=COALESCE($8,status) WHERE id=$1`,
			id, req.Name, req.TeacherID, req.ClassroomID, req.StartDate, req.EndDate, req.Capacity, req.Status); err != nil {
			return err
		}
		// смена преподавателя группы переносится на будущие незакрытые занятия серии
		if req.TeacherID != nil && deref(oldTeacher) != *req.TeacherID {
			tx.Exec(ctx, `UPDATE lessons SET teacher_id=NULLIF($2,'')::uuid WHERE group_id=$1 AND status IN ('planned','needs_decision') AND starts_at > NOW() AND NOT is_detached`, id, *req.TeacherID)
			tx.Exec(ctx, `UPDATE schedules SET teacher_id=NULLIF($2,'')::uuid WHERE group_id=$1 AND (valid_to IS NULL OR valid_to >= CURRENT_DATE)`, id, *req.TeacherID)
		}
		if req.Status != nil && *req.Status == "archived" {
			tx.Exec(ctx, `UPDATE lessons SET status='cancelled' WHERE group_id=$1 AND status='planned' AND starts_at > NOW()`, id)
			tx.Exec(ctx, `UPDATE schedules SET valid_to=CURRENT_DATE WHERE group_id=$1 AND (valid_to IS NULL OR valid_to > CURRENT_DATE)`, id)
		}
		return database.Audit(ctx, tx, "group", id, "update", map[string]any{"teacher_id": oldTeacher}, req)
	})
}

func (h *Handler) addWaitlist(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		StudentID string `json:"student_id"`
		LeadID    string `json:"lead_id"`
		Priority  int    `json:"priority"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.StudentID == "" && req.LeadID == "" {
		return httpx.BadRequest("VALIDATION_FAILED", "Укажите ученика или лида")
	}
	return h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		_, err := tx.Exec(r.Context(), `INSERT INTO group_waitlist (tenant_id, group_id, student_id, lead_id, priority) VALUES (app_current_tenant(), $1, NULLIF($2,'')::uuid, NULLIF($3,'')::uuid, $4)`,
			chi.URLParam(r, "id"), req.StudentID, req.LeadID, req.Priority)
		return err
	})
}

func (h *Handler) removeWaitlist(w http.ResponseWriter, r *http.Request) error {
	return h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		_, err := tx.Exec(r.Context(), `DELETE FROM group_waitlist WHERE id=$1 AND group_id=$2`, chi.URLParam(r, "wid"), chi.URLParam(r, "id"))
		return err
	})
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
