package student

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"emp/internal/modules/customfield"
	"emp/internal/platform/auth"
	"emp/internal/platform/database"
	"emp/internal/platform/httpx"
	mw "emp/internal/platform/middleware"
	"emp/internal/platform/validate"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

type Handler struct {
	DB      *database.DB
	BotName string
}

var Statuses = []string{"registration", "active", "frozen", "graduated", "inactive", "archived"}

func (h *Handler) Routes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(mw.Perm("students.view"))
		r.Get("/students", httpx.Handler(h.list).ServeHTTP)
		r.Get("/students/duplicates", httpx.Handler(h.duplicates).ServeHTTP)
		r.Get("/students/{id}", httpx.Handler(h.get).ServeHTTP)
		r.Get("/students/{id}/history", httpx.Handler(h.history).ServeHTTP)
		r.Post("/students/{id}/reveal", httpx.Handler(h.reveal).ServeHTTP)
		r.Get("/saved-views", httpx.Handler(h.listViews).ServeHTTP)
		r.Post("/saved-views", httpx.Handler(h.createView).ServeHTTP)
		r.Delete("/saved-views/{id}", httpx.Handler(h.deleteView).ServeHTTP)
		r.With(mw.Perm("students.export")).Get("/students/export", httpx.Handler(h.export).ServeHTTP)
	})
	r.Group(func(r chi.Router) {
		r.Use(mw.Perm("students.edit"))
		r.Post("/students", httpx.Handler(h.create).ServeHTTP)
		r.Patch("/students/{id}", httpx.Handler(h.update).ServeHTTP)
		r.Post("/students/{id}/archive", httpx.Handler(h.archive).ServeHTTP)
		r.Post("/students/{id}/restore", httpx.Handler(h.restore).ServeHTTP)
		r.Post("/students/{id}/comments", httpx.Handler(h.addComment).ServeHTTP)
		r.Post("/students/{id}/parents", httpx.Handler(h.linkParent).ServeHTTP)
		r.Delete("/students/{id}/parents/{parentId}", httpx.Handler(h.unlinkParent).ServeHTTP)
		r.Post("/students/{id}/groups", httpx.Handler(h.addToGroup).ServeHTTP)
		r.Post("/students/{id}/groups/{groupId}/leave", httpx.Handler(h.leaveGroup).ServeHTTP)
		r.Post("/students/bulk", httpx.Handler(h.bulk).ServeHTTP)
		r.Post("/students/merge", httpx.Handler(h.merge).ServeHTTP)
	})
	r.With(mw.Perm("students.import")).Post("/students/import", httpx.Handler(h.importFile).ServeHTTP)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) error {
	p := auth.FromContext(r.Context())
	limit := httpx.QueryInt(r, "limit", 50, 200)
	var rows []Row
	var next string
	var total int
	var fields []customfield.Field
	err := h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		var err error
		if fields, err = customfield.Load(r.Context(), tx, "student", false); err != nil {
			return err
		}
		rows, next, total, err = List(r.Context(), tx, r.URL.Query(), fields, p, limit)
		return err
	})
	if err != nil {
		return err
	}
	Present(rows, fields, p)
	return httpx.OKMeta(w, rows, map[string]any{"total": total, "next_cursor": next})
}

// duplicates — поиск дубликатов по телефону среди учеников и лидов (ТЗ 5.4).
func (h *Handler) duplicates(w http.ResponseWriter, r *http.Request) error {
	phone, ok := validate.Phone(r.URL.Query().Get("phone"))
	if !ok || phone == "" {
		return httpx.OK(w, []any{})
	}
	type dup struct {
		Kind     string `json:"kind"`
		ID       string `json:"id"`
		FullName string `json:"full_name"`
		Status   string `json:"status"`
	}
	out := []dup{}
	err := h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `SELECT 'student', id, full_name, status FROM students WHERE phone=$1
			UNION ALL SELECT 'parent', id, full_name, '' FROM parents WHERE phone=$1
			UNION ALL SELECT 'lead', id, full_name, stage FROM leads WHERE phone=$1 AND student_id IS NULL LIMIT 20`, phone)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var d dup
			rows.Scan(&d.Kind, &d.ID, &d.FullName, &d.Status)
			out = append(out, d)
		}
		return rows.Err()
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, out)
}

type parentInput struct {
	ID         string `json:"id"`
	FullName   string `json:"full_name"`
	Phone      string `json:"phone"`
	Email      string `json:"email"`
	Relation   string `json:"relation"`
	IsPayer    bool   `json:"is_payer"`
	IsLegalRep bool   `json:"is_legal_rep"`
	Consent    bool   `json:"consent"`
}

type input struct {
	FullName      *string        `json:"full_name"`
	Phone         *string        `json:"phone"`
	Email         *string        `json:"email"`
	BirthDate     *string        `json:"birth_date"`
	Gender        *string        `json:"gender"`
	Address       *string        `json:"address"`
	IIN           *string        `json:"iin"`
	Source        *string        `json:"source"`
	BranchID      *string        `json:"branch_id"`
	Status        *string        `json:"status"`
	ResponsibleID *string        `json:"responsible_id"`
	Comment       *string        `json:"comment"`
	CustomFields  map[string]any `json:"custom_fields"`
	Version       *int           `json:"version"`
	// только при создании
	GroupID string        `json:"group_id"`
	Parents []parentInput `json:"parents"`
	LeadID  string        `json:"lead_id"`
}

// normalize проверяет стандартные поля и приводит их к формату хранения.
func (in *input) normalize(v *httpx.Validation) {
	if in.FullName != nil {
		n := strings.Join(strings.Fields(*in.FullName), " ")
		in.FullName = &n
		if n == "" {
			v.Add("full_name", "REQUIRED")
		}
	}
	if in.Phone != nil {
		ph, ok := validate.Phone(*in.Phone)
		if !ok {
			v.Add("phone", "INVALID_FORMAT")
		}
		in.Phone = &ph
	}
	if in.Email != nil {
		e := strings.ToLower(strings.TrimSpace(*in.Email))
		if !validate.Email(e) {
			v.Add("email", "INVALID_FORMAT")
		}
		in.Email = &e
	}
	if in.IIN != nil {
		s := strings.TrimSpace(*in.IIN)
		if s == customfield.Masked || strings.Contains(s, "•") {
			in.IIN = nil // маска вернулась с клиента — не меняем
		} else {
			if s != "" && !validate.IIN(s) {
				v.Add("iin", "INVALID_IIN")
			}
			in.IIN = &s
		}
	}
	if in.BirthDate != nil && *in.BirthDate != "" {
		if _, err := time.Parse("2006-01-02", *in.BirthDate); err != nil {
			v.Add("birth_date", "INVALID_DATE")
		}
	}
	if in.Status != nil && !slices.Contains(Statuses, *in.Status) {
		v.Add("status", "INVALID")
	}
}

func nz(s *string) *string {
	if s == nil {
		return nil
	}
	return httpx.Ptr(*s)
}

// checkPlanLimit: при превышении лимита активных учеников даётся 14 дней, затем блокируется только создание (ТЗ 23.3).
func checkPlanLimit(ctx context.Context, tx pgx.Tx) error {
	var max *int
	var since *time.Time
	if err := tx.QueryRow(ctx, `SELECT sp.max_students, t.over_limit_since FROM tenants t JOIN subscription_plans sp ON sp.code=t.plan_code WHERE t.id = app_current_tenant()`).Scan(&max, &since); err != nil {
		return err
	}
	if max == nil {
		return nil
	}
	var active int
	tx.QueryRow(ctx, `SELECT count(*) FROM students WHERE status IN ('active','registration') AND archived_at IS NULL`).Scan(&active)
	if active < *max {
		if since != nil {
			tx.Exec(ctx, `UPDATE tenants SET over_limit_since=NULL WHERE id=app_current_tenant()`)
		}
		return nil
	}
	if since == nil {
		_, err := tx.Exec(ctx, `UPDATE tenants SET over_limit_since=NOW() WHERE id=app_current_tenant()`)
		return err
	}
	if time.Since(*since) > 14*24*time.Hour {
		return httpx.Conflict("PLAN_LIMIT", "Превышен лимит активных учеников тарифа. Смените тариф, чтобы добавлять новых учеников")
	}
	return nil
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) error {
	var in input
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	p := auth.FromContext(r.Context())
	ctx := r.Context()
	v := &httpx.Validation{}
	if in.FullName == nil {
		v.Add("full_name", "REQUIRED")
	}
	in.normalize(v)
	if in.BranchID == nil || *in.BranchID == "" {
		v.Add("branch_id", "REQUIRED")
	} else if !p.CanBranch(*in.BranchID) {
		return httpx.Forbidden()
	}
	if err := v.Err(); err != nil {
		return err
	}
	status := "registration"
	if in.Status != nil {
		status = *in.Status
	}
	var id string
	err := h.DB.Tx(ctx, func(tx pgx.Tx) error {
		if err := checkPlanLimit(ctx, tx); err != nil {
			return err
		}
		fields, err := customfield.Load(ctx, tx, "student", false)
		if err != nil {
			return err
		}
		var courseIDs []string
		if in.GroupID != "" {
			var c string
			if err := tx.QueryRow(ctx, `SELECT course_id FROM groups WHERE id=$1`, in.GroupID).Scan(&c); err != nil {
				return httpx.BadRequest("GROUP_NOT_FOUND", "Группа не найдена")
			}
			courseIDs = []string{c}
		}
		cf, err := customfield.Apply(ctx, tx, fields, nil, in.CustomFields, p, customfield.Context{
			Table: "students", Status: status, CourseIDs: courseIDs, BranchID: *in.BranchID, Creating: true})
		if err != nil {
			return err
		}
		cfJSON, _ := json.Marshal(cf)
		var started *string
		if status == "active" {
			t := time.Now().Format("2006-01-02")
			started = &t
		}
		if err := tx.QueryRow(ctx, `INSERT INTO students (tenant_id, branch_id, full_name, birth_date, gender, phone, email, address, iin, source, responsible_id, status, comment, custom_fields, lead_id, started_at)
			VALUES (app_current_tenant(), $1, $2, NULLIF($3,'')::date, $4, $5, $6, $7, $8, $9, NULLIF($10,'')::uuid, $11, $12, $13, NULLIF($14,'')::uuid, $15::date) RETURNING id`,
			*in.BranchID, *in.FullName, deref(in.BirthDate), nz(in.Gender), nz(in.Phone), nz(in.Email), nz(in.Address), nz(in.IIN), nz(in.Source),
			deref(in.ResponsibleID), status, nz(in.Comment), cfJSON, in.LeadID, started).Scan(&id); err != nil {
			return err
		}
		tx.Exec(ctx, `INSERT INTO student_balances (tenant_id, student_id) VALUES (app_current_tenant(), $1)`, id)
		for _, pi := range in.Parents {
			if err := attachParent(ctx, tx, id, pi); err != nil {
				return err
			}
		}
		if in.GroupID != "" {
			if err := joinGroup(ctx, tx, id, in.GroupID, ""); err != nil {
				return err
			}
		}
		if err := database.Audit(ctx, tx, "student", id, "create", nil, map[string]any{"full_name": *in.FullName, "status": status}); err != nil {
			return err
		}
		return database.Publish(ctx, tx, "student.created", map[string]any{"student_id": id})
	})
	if err != nil {
		return err
	}
	return httpx.Created(w, map[string]string{"id": id})
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// attachParent создаёт родителя (или находит по телефону) и связывает с учеником.
func attachParent(ctx context.Context, tx pgx.Tx, studentID string, pi parentInput) error {
	pid := pi.ID
	if pid == "" {
		name := strings.Join(strings.Fields(pi.FullName), " ")
		phone, ok := validate.Phone(pi.Phone)
		if name == "" || !ok {
			return (&httpx.Validation{Fields: []httpx.FieldError{{Field: "parents", Code: "INVALID"}}}).Err()
		}
		if phone != "" {
			tx.QueryRow(ctx, `SELECT id FROM parents WHERE phone=$1 AND archived_at IS NULL LIMIT 1`, phone).Scan(&pid)
		}
		if pid == "" {
			var consentAt *time.Time
			if pi.Consent {
				t := time.Now()
				consentAt = &t
			}
			if err := tx.QueryRow(ctx, `INSERT INTO parents (tenant_id, full_name, phone, whatsapp, email, telegram_link_code, consent_at, consent_method, consent_version)
				VALUES (app_current_tenant(), $1, NULLIF($2,''), NULLIF($2,''), NULLIF($3,''), $4, $5, CASE WHEN $5::timestamptz IS NOT NULL THEN 'form' END, CASE WHEN $5::timestamptz IS NOT NULL THEN 'v1' END) RETURNING id`,
				name, phone, strings.ToLower(strings.TrimSpace(pi.Email)), auth.RandomCode(16), consentAt).Scan(&pid); err != nil {
				return err
			}
		}
	}
	rel := pi.Relation
	if rel == "" {
		rel = "parent"
	}
	if _, err := tx.Exec(ctx, `INSERT INTO student_parents (tenant_id, student_id, parent_id, relation, is_payer, is_legal_rep)
		VALUES (app_current_tenant(), $1, $2, $3, $4, $5)
		ON CONFLICT (student_id, parent_id) DO UPDATE SET relation=EXCLUDED.relation, is_payer=EXCLUDED.is_payer, is_legal_rep=EXCLUDED.is_legal_rep`,
		studentID, pid, rel, pi.IsPayer, pi.IsLegalRep); err != nil {
		return err
	}
	return database.Audit(ctx, tx, "student", studentID, "parent_linked", nil, map[string]any{"parent_id": pid, "relation": rel})
}

func joinGroup(ctx context.Context, tx pgx.Tx, studentID, groupID, date string) error {
	var capacity *int
	var count int
	var gtype string
	if err := tx.QueryRow(ctx, `SELECT capacity, type, (SELECT count(*) FROM group_students WHERE group_id=g.id AND left_at IS NULL) FROM groups g WHERE id=$1 AND status='active'`, groupID).
		Scan(&capacity, &gtype, &count); err != nil {
		return httpx.BadRequest("GROUP_NOT_FOUND", "Группа не найдена или в архиве")
	}
	if capacity != nil && count >= *capacity {
		return httpx.Conflict("GROUP_FULL", "В группе нет свободных мест — добавьте ученика в лист ожидания")
	}
	if _, err := tx.Exec(ctx, `INSERT INTO group_students (tenant_id, group_id, student_id, joined_at) VALUES (app_current_tenant(), $1, $2, COALESCE(NULLIF($3,'')::date, CURRENT_DATE))`,
		groupID, studentID, date); err != nil {
		return err
	}
	return database.Audit(ctx, tx, "student", studentID, "group_joined", nil, map[string]any{"group_id": groupID})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	p := auth.FromContext(r.Context())
	ctx := r.Context()
	out := map[string]any{}
	err := h.DB.Tx(ctx, func(tx pgx.Tx) error {
		fields, err := customfield.Load(ctx, tx, "student", false)
		if err != nil {
			return err
		}
		row, err := GetRow(ctx, tx, id)
		if err != nil {
			return err
		}
		if !p.CanBranch(row.BranchID) {
			return httpx.NotFound("STUDENT_NOT_FOUND")
		}
		rows := []Row{*row}
		Present(rows, fields, p)
		out["student"] = rows[0]
		var extra struct {
			Gender, Address, Comment *string
			ResponsibleID            *string
			Version                  int
			StartedAt                *time.Time
			LeadID                   *string
		}
		var branchName string
		var respName *string
		tx.QueryRow(ctx, `SELECT s.gender, s.address, s.comment, s.responsible_id, s.version, s.started_at, s.lead_id, b.name, u.full_name
			FROM students s JOIN branches b ON b.id=s.branch_id LEFT JOIN users u ON u.id=s.responsible_id WHERE s.id=$1`, id).
			Scan(&extra.Gender, &extra.Address, &extra.Comment, &extra.ResponsibleID, &extra.Version, &extra.StartedAt, &extra.LeadID, &branchName, &respName)
		out["details"] = map[string]any{"gender": extra.Gender, "address": extra.Address, "comment": extra.Comment, "responsible_id": extra.ResponsibleID,
			"responsible_name": respName, "version": extra.Version, "started_at": extra.StartedAt, "lead_id": extra.LeadID, "branch_name": branchName}
		out["fields"] = customfield.Visible(fields, p)

		// родители
		prow, err := tx.Query(ctx, `SELECT p.id, p.full_name, p.phone, p.email, p.telegram_chat_id IS NOT NULL, p.telegram_link_code, sp.relation, sp.is_payer, sp.is_legal_rep, p.consent_at
			FROM student_parents sp JOIN parents p ON p.id=sp.parent_id WHERE sp.student_id=$1 ORDER BY sp.is_payer DESC, p.full_name`, id)
		if err != nil {
			return err
		}
		parents := []map[string]any{}
		for prow.Next() {
			var pid, name, rel string
			var phone, email, code *string
			var tg, payer, legal bool
			var consent *time.Time
			prow.Scan(&pid, &name, &phone, &email, &tg, &code, &rel, &payer, &legal, &consent)
			var link *string
			if code != nil {
				l := "https://t.me/" + h.BotName + "?start=" + *code
				link = &l
			}
			parents = append(parents, map[string]any{"id": pid, "full_name": name, "phone": phone, "email": email, "telegram_linked": tg,
				"telegram_link_code": code, "telegram_link": link, "relation": rel, "is_payer": payer, "is_legal_rep": legal, "consent_at": consent})
		}
		prow.Close()
		out["parents"] = parents

		// группы с расписанием
		grow, err := tx.Query(ctx, `SELECT g.id, g.name, c.name, g.type, e.full_name, cl.name, gs.joined_at,
			(SELECT string_agg(DISTINCT array_to_string(sc.weekdays, ',') || ' ' || to_char(sc.start_time, 'HH24:MI'), '; ') FROM schedules sc WHERE sc.group_id=g.id AND (sc.valid_to IS NULL OR sc.valid_to >= CURRENT_DATE))
			FROM group_students gs JOIN groups g ON g.id=gs.group_id JOIN courses c ON c.id=g.course_id
			LEFT JOIN employees e ON e.id=g.teacher_id LEFT JOIN classrooms cl ON cl.id=g.classroom_id
			WHERE gs.student_id=$1 AND gs.left_at IS NULL ORDER BY g.name`, id)
		if err != nil {
			return err
		}
		groups := []map[string]any{}
		for grow.Next() {
			var gid, gname, cname, gtype string
			var teacher, room, sched *string
			var joined time.Time
			grow.Scan(&gid, &gname, &cname, &gtype, &teacher, &room, &joined, &sched)
			groups = append(groups, map[string]any{"id": gid, "name": gname, "course": cname, "type": gtype, "teacher": teacher, "classroom": room, "joined_at": joined, "schedule": sched})
		}
		grow.Close()
		out["groups"] = groups

		// последние 12 отметок посещаемости
		arow, err := tx.Query(ctx, `SELECT l.starts_at, a.status, a.reason FROM attendance a JOIN lessons l ON l.id=a.lesson_id
			WHERE a.student_id=$1 ORDER BY l.starts_at DESC LIMIT 12`, id)
		if err != nil {
			return err
		}
		att := []map[string]any{}
		for arow.Next() {
			var t time.Time
			var st string
			var reason *string
			arow.Scan(&t, &st, &reason)
			att = append([]map[string]any{{"date": t, "status": st, "reason": reason}}, att...)
		}
		arow.Close()
		var rate *float64
		tx.QueryRow(ctx, `SELECT round(100.0 * count(*) FILTER (WHERE status IN ('present','late')) / NULLIF(count(*) FILTER (WHERE status <> 'trial'),0))
			FROM attendance WHERE student_id=$1`, id).Scan(&rate)
		out["attendance"] = map[string]any{"recent": att, "rate": rate}

		// абонементы
		if p.Can("finance.view") {
			srow, err := tx.Query(ctx, `SELECT sb.id, sb.name, sb.type, sb.price_tiyn, sb.lessons_total, sb.starts_on, sb.ends_on, sb.status,
				coalesce((SELECT sum(qty) FROM subscription_usages u WHERE u.subscription_id=sb.id),0)
				FROM subscriptions sb WHERE sb.student_id=$1 ORDER BY sb.starts_on DESC LIMIT 10`, id)
			if err != nil {
				return err
			}
			subs := []map[string]any{}
			for srow.Next() {
				var sid, name, typ, st string
				var price int64
				var total *int
				var from time.Time
				var to *time.Time
				var used int
				srow.Scan(&sid, &name, &typ, &price, &total, &from, &to, &st, &used)
				subs = append(subs, map[string]any{"id": sid, "name": name, "type": typ, "price_tiyn": price, "lessons_total": total,
					"starts_on": from, "ends_on": to, "status": st, "lessons_used": used})
			}
			srow.Close()
			out["subscriptions"] = subs
		}
		crow, err := tx.Query(ctx, `SELECT c.id, c.body, c.created_at, u.full_name FROM student_comments c LEFT JOIN users u ON u.id=c.author_id
			WHERE c.student_id=$1 ORDER BY c.created_at DESC LIMIT 50`, id)
		if err != nil {
			return err
		}
		comments := []map[string]any{}
		for crow.Next() {
			var cid, body string
			var at time.Time
			var author *string
			crow.Scan(&cid, &body, &at, &author)
			comments = append(comments, map[string]any{"id": cid, "body": body, "created_at": at, "author": author})
		}
		crow.Close()
		out["comments"] = comments
		return nil
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, out)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	var in input
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	if in.Version == nil {
		if v, err := strconv.Atoi(strings.Trim(r.Header.Get("If-Match"), `"`)); err == nil {
			in.Version = &v
		}
	}
	p := auth.FromContext(r.Context())
	ctx := r.Context()
	v := &httpx.Validation{}
	in.normalize(v)
	if err := v.Err(); err != nil {
		return err
	}
	var newVersion int
	err := h.DB.Tx(ctx, func(tx pgx.Tx) error {
		var cur struct {
			FullName, Status, BranchID string
			Phone, Email, IIN, Source  *string
			BirthDate                  *time.Time
			CF                         map[string]any
			Version                    int
		}
		if err := tx.QueryRow(ctx, `SELECT full_name, status, branch_id, phone, email, iin, source, birth_date, custom_fields, version FROM students WHERE id=$1 FOR UPDATE`, id).
			Scan(&cur.FullName, &cur.Status, &cur.BranchID, &cur.Phone, &cur.Email, &cur.IIN, &cur.Source, &cur.BirthDate, &cur.CF, &cur.Version); err != nil {
			return httpx.NotFound("STUDENT_NOT_FOUND")
		}
		if !p.CanBranch(cur.BranchID) {
			return httpx.NotFound("STUDENT_NOT_FOUND")
		}
		// оптимистичная блокировка: два администратора не перезапишут изменения друг друга
		if in.Version != nil && *in.Version != cur.Version {
			return httpx.Conflict("VERSION_CONFLICT", "Карточку уже изменил другой пользователь. Обновите страницу")
		}
		if in.IIN != nil && !p.Can("students.view_iin") && *in.IIN != "" {
			return httpx.Forbidden()
		}
		if in.BranchID != nil && !p.CanBranch(*in.BranchID) {
			return httpx.Forbidden()
		}
		status := cur.Status
		if in.Status != nil {
			status = *in.Status
		}
		oldV, newV := map[string]any{}, map[string]any{}
		diff := func(key string, old, nw any) {
			if !equal(old, nw) {
				oldV[key], newV[key] = old, nw
			}
		}
		if in.CustomFields != nil || status != cur.Status {
			fields, err := customfield.Load(ctx, tx, "student", false)
			if err != nil {
				return err
			}
			var courseIDs []string
			rows, _ := tx.Query(ctx, `SELECT g.course_id::text FROM group_students gs JOIN groups g ON g.id=gs.group_id WHERE gs.student_id=$1 AND gs.left_at IS NULL`, id)
			courseIDs, _ = pgx.CollectRows(rows, pgx.RowTo[string])
			cf, err := customfield.Apply(ctx, tx, fields, cur.CF, in.CustomFields, p, customfield.Context{
				Table: "students", RecordID: id, Status: status, CourseIDs: courseIDs, BranchID: cur.BranchID})
			if err != nil {
				return err
			}
			for k, nv := range cf {
				diff("cf."+k, cur.CF[k], nv)
			}
			for k, ov := range cur.CF {
				if _, ok := cf[k]; !ok {
					diff("cf."+k, ov, nil)
				}
			}
			cfJSON, _ := json.Marshal(cf)
			if _, err := tx.Exec(ctx, `UPDATE students SET custom_fields=$2 WHERE id=$1`, id, cfJSON); err != nil {
				return err
			}
		}
		set := func(col string, val any, old any) error {
			diff(col, old, val)
			_, err := tx.Exec(ctx, "UPDATE students SET "+col+"=$2 WHERE id=$1", id, val)
			return err
		}
		if in.FullName != nil {
			if err := set("full_name", *in.FullName, cur.FullName); err != nil {
				return err
			}
		}
		if in.Phone != nil {
			if err := set("phone", nz(in.Phone), cur.Phone); err != nil {
				return err
			}
		}
		if in.Email != nil {
			if err := set("email", nz(in.Email), cur.Email); err != nil {
				return err
			}
		}
		if in.IIN != nil {
			diff("iin", maskPtr(cur.IIN), maskPtr(nz(in.IIN)))
			if _, err := tx.Exec(ctx, "UPDATE students SET iin=$2 WHERE id=$1", id, nz(in.IIN)); err != nil {
				return err
			}
		}
		if in.Source != nil {
			if err := set("source", nz(in.Source), cur.Source); err != nil {
				return err
			}
		}
		if in.BirthDate != nil {
			var old *string
			if cur.BirthDate != nil {
				s := cur.BirthDate.Format("2006-01-02")
				old = &s
			}
			diff("birth_date", old, nz(in.BirthDate))
			if _, err := tx.Exec(ctx, "UPDATE students SET birth_date=NULLIF($2,'')::date WHERE id=$1", id, *in.BirthDate); err != nil {
				return err
			}
		}
		for col, val := range map[string]*string{"gender": in.Gender, "address": in.Address, "comment": in.Comment} {
			if val != nil {
				if _, err := tx.Exec(ctx, "UPDATE students SET "+col+"=$2 WHERE id=$1", id, nz(val)); err != nil {
					return err
				}
				newV[col] = nz(val)
			}
		}
		if in.ResponsibleID != nil {
			if _, err := tx.Exec(ctx, "UPDATE students SET responsible_id=NULLIF($2,'')::uuid WHERE id=$1", id, *in.ResponsibleID); err != nil {
				return err
			}
			newV["responsible_id"] = *in.ResponsibleID
		}
		if in.BranchID != nil && *in.BranchID != cur.BranchID {
			if err := set("branch_id", *in.BranchID, cur.BranchID); err != nil {
				return err
			}
		}
		if status != cur.Status {
			if err := set("status", status, cur.Status); err != nil {
				return err
			}
			if status == "active" {
				tx.Exec(ctx, `UPDATE students SET started_at=COALESCE(started_at, CURRENT_DATE) WHERE id=$1`, id)
			}
			if err := database.Publish(ctx, tx, "student.status_changed", map[string]any{"student_id": id, "from": cur.Status, "to": status}); err != nil {
				return err
			}
		}
		if err := tx.QueryRow(ctx, `UPDATE students SET version=version+1, updated_at=NOW() WHERE id=$1 RETURNING version`, id).Scan(&newVersion); err != nil {
			return err
		}
		if len(newV) == 0 {
			return nil
		}
		return database.Audit(ctx, tx, "student", id, "update", oldV, newV)
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{"id": id, "version": newVersion})
}

func maskPtr(s *string) *string {
	if s == nil || *s == "" {
		return nil
	}
	m := validate.MaskIIN(*s)
	return &m
}

func equal(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

// reveal показывает полное значение ИИН или чувствительного поля и пишет просмотр в журнал доступа (ТЗ 6.2, 9.4).
func (h *Handler) reveal(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	var req struct {
		Field string `json:"field"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	p := auth.FromContext(r.Context())
	ctx := r.Context()
	var value any
	err := h.DB.Tx(ctx, func(tx pgx.Tx) error {
		if req.Field == "iin" {
			if !p.Can("students.view_iin") {
				return httpx.Forbidden()
			}
			var iin *string
			if err := tx.QueryRow(ctx, `SELECT iin FROM students WHERE id=$1`, id).Scan(&iin); err != nil {
				return err
			}
			value = iin
		} else {
			key := strings.TrimPrefix(req.Field, "cf.")
			fields, err := customfield.Load(ctx, tx, "student", false)
			if err != nil {
				return err
			}
			f := customfield.ByKey(fields)[key]
			if f == nil || !customfield.CanView(f, p) {
				return httpx.Forbidden()
			}
			if err := tx.QueryRow(ctx, `SELECT custom_fields->$2 FROM students WHERE id=$1`, id, key).Scan(&value); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `INSERT INTO access_logs (tenant_id, user_id, entity_type, entity_id, field) VALUES (app_current_tenant(), $1, 'student', $2, $3)`,
			p.UserID, id, req.Field)
		return err
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]any{"value": value})
}

func (h *Handler) history(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	limit := httpx.QueryInt(r, "limit", 30, 200)
	out := []map[string]any{}
	err := h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `SELECT a.id, a.action, a.old_values, a.new_values, a.created_at, coalesce(u.full_name, 'Система')
			FROM audit_logs a LEFT JOIN users u ON u.id=a.user_id
			WHERE a.entity_type='student' AND a.entity_id=$1 ORDER BY a.created_at DESC, a.id DESC LIMIT $2`, id, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var aid int64
			var action, user string
			var oldV, newV map[string]any
			var at time.Time
			rows.Scan(&aid, &action, &oldV, &newV, &at, &user)
			out = append(out, map[string]any{"id": aid, "action": action, "old": oldV, "new": newV, "created_at": at, "user": user})
		}
		return rows.Err()
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, out)
}

func (h *Handler) setArchived(r *http.Request, archived bool) error {
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	return h.DB.Tx(ctx, func(tx pgx.Tx) error {
		var q string
		if archived {
			q = `UPDATE students SET archived_at=NOW(), status='archived', version=version+1 WHERE id=$1 AND archived_at IS NULL`
		} else {
			q = `UPDATE students SET archived_at=NULL, status='inactive', version=version+1 WHERE id=$1`
		}
		tag, err := tx.Exec(ctx, q, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.NotFound("STUDENT_NOT_FOUND")
		}
		if archived {
			tx.Exec(ctx, `UPDATE group_students SET left_at=CURRENT_DATE, leave_reason='archived' WHERE student_id=$1 AND left_at IS NULL`, id)
			return database.Audit(ctx, tx, "student", id, "archive", nil, nil)
		}
		return database.Audit(ctx, tx, "student", id, "restore", nil, nil)
	})
}

func (h *Handler) archive(w http.ResponseWriter, r *http.Request) error {
	if err := h.setArchived(r, true); err != nil {
		return err
	}
	return httpx.NoContent(w)
}

func (h *Handler) restore(w http.ResponseWriter, r *http.Request) error {
	if err := h.setArchived(r, false); err != nil {
		return err
	}
	return httpx.NoContent(w)
}

func (h *Handler) addComment(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Body string `json:"body"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if strings.TrimSpace(req.Body) == "" {
		return httpx.BadRequest("VALIDATION_FAILED", "Пустой комментарий")
	}
	p := auth.FromContext(r.Context())
	return h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		_, err := tx.Exec(r.Context(), `INSERT INTO student_comments (tenant_id, student_id, author_id, body) VALUES (app_current_tenant(), $1, $2, $3)`,
			chi.URLParam(r, "id"), p.UserID, strings.TrimSpace(req.Body))
		return err
	})
}

func (h *Handler) linkParent(w http.ResponseWriter, r *http.Request) error {
	var pi parentInput
	if err := httpx.Decode(r, &pi); err != nil {
		return err
	}
	return h.DB.Tx(r.Context(), func(tx pgx.Tx) error { return attachParent(r.Context(), tx, chi.URLParam(r, "id"), pi) })
}

func (h *Handler) unlinkParent(w http.ResponseWriter, r *http.Request) error {
	id, pid := chi.URLParam(r, "id"), chi.URLParam(r, "parentId")
	return h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), `DELETE FROM student_parents WHERE student_id=$1 AND parent_id=$2`, id, pid); err != nil {
			return err
		}
		return database.Audit(r.Context(), tx, "student", id, "parent_unlinked", map[string]any{"parent_id": pid}, nil)
	})
}

func (h *Handler) addToGroup(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		GroupID   string `json:"group_id"`
		JoinedAt  string `json:"joined_at"`
		FromGroup string `json:"from_group_id"` // перевод из другой группы
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	id := chi.URLParam(r, "id")
	return h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		if req.FromGroup != "" {
			if _, err := tx.Exec(r.Context(), `UPDATE group_students SET left_at=COALESCE(NULLIF($3,'')::date, CURRENT_DATE), leave_reason='transfer'
				WHERE student_id=$1 AND group_id=$2 AND left_at IS NULL`, id, req.FromGroup, req.JoinedAt); err != nil {
				return err
			}
		}
		return joinGroup(r.Context(), tx, id, req.GroupID, req.JoinedAt)
	})
}

func (h *Handler) leaveGroup(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Reason string `json:"reason"`
	}
	httpx.Decode(r, &req)
	id, gid := chi.URLParam(r, "id"), chi.URLParam(r, "groupId")
	return h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		tag, err := tx.Exec(r.Context(), `UPDATE group_students SET left_at=CURRENT_DATE, leave_reason=NULLIF($3,'') WHERE student_id=$1 AND group_id=$2 AND left_at IS NULL`, id, gid, req.Reason)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.NotFound("MEMBERSHIP_NOT_FOUND")
		}
		if err := database.Publish(r.Context(), tx, "group.seat_freed", map[string]any{"group_id": gid}); err != nil {
			return err
		}
		return database.Audit(r.Context(), tx, "student", id, "group_left", map[string]any{"group_id": gid}, map[string]any{"reason": req.Reason})
	})
}

// ---------------------------------------------------------------- «Виды»

func (h *Handler) listViews(w http.ResponseWriter, r *http.Request) error {
	entity := r.URL.Query().Get("entity")
	if entity == "" {
		entity = "student"
	}
	p := auth.FromContext(r.Context())
	out := []map[string]any{}
	err := h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `SELECT id, name, config, user_id IS NULL FROM saved_views WHERE entity_type=$1 AND (user_id IS NULL OR user_id=$2) ORDER BY user_id NULLS FIRST, sort_order, created_at`, entity, p.UserID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id, name string
			var cfg map[string]any
			var shared bool
			rows.Scan(&id, &name, &cfg, &shared)
			out = append(out, map[string]any{"id": id, "name": name, "config": cfg, "shared": shared})
		}
		return rows.Err()
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, out)
}

func (h *Handler) createView(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Entity string         `json:"entity_type"`
		Name   string         `json:"name"`
		Config map[string]any `json:"config"`
		Shared bool           `json:"shared"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if strings.TrimSpace(req.Name) == "" {
		return httpx.BadRequest("VALIDATION_FAILED", "Укажите название вида")
	}
	if req.Entity == "" {
		req.Entity = "student"
	}
	p := auth.FromContext(r.Context())
	if req.Shared && !p.Can("settings.manage") {
		return httpx.Forbidden()
	}
	var uid *string
	if !req.Shared {
		uid = &p.UserID
	}
	var id string
	err := h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		cfg, _ := json.Marshal(req.Config)
		return tx.QueryRow(r.Context(), `INSERT INTO saved_views (tenant_id, user_id, entity_type, name, config, sort_order) VALUES (app_current_tenant(), $1, $2, $3, $4, 100) RETURNING id`,
			uid, req.Entity, strings.TrimSpace(req.Name), cfg).Scan(&id)
	})
	if err != nil {
		return err
	}
	return httpx.Created(w, map[string]string{"id": id})
}

func (h *Handler) deleteView(w http.ResponseWriter, r *http.Request) error {
	p := auth.FromContext(r.Context())
	return h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		_, err := tx.Exec(r.Context(), `DELETE FROM saved_views WHERE id=$1 AND (user_id=$2 OR (user_id IS NULL AND $3))`, chi.URLParam(r, "id"), p.UserID, p.Can("settings.manage"))
		return err
	})
}
