// Пакет lead — лиды и простая воронка с конвертацией в ученика (ТЗ раздел 5, MVP-объём).
package lead

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"emp/internal/modules/customfield"
	"emp/internal/modules/finance"
	"emp/internal/platform/auth"
	"emp/internal/platform/database"
	"emp/internal/platform/httpx"
	mw "emp/internal/platform/middleware"
	"emp/internal/platform/validate"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

type Handler struct{ DB *database.DB }

var Stages = []string{"new", "contacted", "trial_scheduled", "trial_done", "won", "lost"}

func (h *Handler) Routes(r chi.Router) {
	r.With(mw.Perm("leads.view")).Get("/leads", httpx.Handler(h.list).ServeHTTP)
	r.With(mw.Perm("leads.view")).Get("/leads/{id}", httpx.Handler(h.get).ServeHTTP)
	r.Group(func(r chi.Router) {
		r.Use(mw.Perm("leads.edit"))
		r.Post("/leads", httpx.Handler(h.create).ServeHTTP)
		r.Patch("/leads/{id}", httpx.Handler(h.update).ServeHTTP)
		r.Post("/leads/{id}/convert", httpx.Handler(h.convert).ServeHTTP)
	})
}

type dto struct {
	ID              string         `json:"id"`
	BranchID        *string        `json:"branch_id"`
	FullName        string         `json:"full_name"`
	ContactName     *string        `json:"contact_name"`
	Phone           string         `json:"phone"`
	Email           *string        `json:"email"`
	Telegram        *string        `json:"telegram"`
	CourseID        *string        `json:"course_id"`
	CourseName      *string        `json:"course_name"`
	PreferredTime   *string        `json:"preferred_time"`
	AgeOrGrade      *string        `json:"age_or_grade"`
	Source          *string        `json:"source"`
	UTM             map[string]any `json:"utm"`
	ResponsibleID   *string        `json:"responsible_id"`
	ResponsibleName *string        `json:"responsible_name"`
	Stage           string         `json:"stage"`
	LostReason      *string        `json:"lost_reason"`
	StudentID       *string        `json:"student_id"`
	LastContactAt   *time.Time     `json:"last_contact_at"`
	Comment         *string        `json:"comment"`
	CF              map[string]any `json:"custom_fields"`
	CreatedAt       time.Time      `json:"created_at"`
}

const sel = `SELECT l.id, l.branch_id, l.full_name, l.contact_name, l.phone, l.email, l.telegram, l.course_id, c.name, l.preferred_time, l.age_or_grade, l.source, l.utm,
	l.responsible_id, u.full_name, l.stage, l.lost_reason, l.student_id, l.last_contact_at, l.comment, l.custom_fields, l.created_at
	FROM leads l LEFT JOIN courses c ON c.id=l.course_id LEFT JOIN users u ON u.id=l.responsible_id`

func scan(rows pgx.Rows) ([]dto, error) {
	defer rows.Close()
	out := []dto{}
	for rows.Next() {
		var d dto
		if err := rows.Scan(&d.ID, &d.BranchID, &d.FullName, &d.ContactName, &d.Phone, &d.Email, &d.Telegram, &d.CourseID, &d.CourseName, &d.PreferredTime, &d.AgeOrGrade,
			&d.Source, &d.UTM, &d.ResponsibleID, &d.ResponsibleName, &d.Stage, &d.LostReason, &d.StudentID, &d.LastContactAt, &d.Comment, &d.CF, &d.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) error {
	p := auth.FromContext(r.Context())
	q := r.URL.Query()
	var out []dto
	counts := map[string]int{}
	err := h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		args := []any{}
		add := func(v any) string { args = append(args, v); return "$" + strconv.Itoa(len(args)) }
		conds := []string{}
		if bf := p.BranchFilter(); bf != nil {
			conds = append(conds, "(l.branch_id IS NULL OR l.branch_id = ANY("+add(bf)+"::uuid[]))")
		}
		// менеджер продаж без общего доступа видит только свои лиды
		if p.HasRole("sales") && !p.AllBranches && !p.Can("settings.manage") && q.Get("mine") != "0" {
			conds = append(conds, "(l.responsible_id = "+add(p.UserID)+"::uuid OR l.responsible_id IS NULL)")
		}
		if s := q.Get("q"); s != "" {
			conds = append(conds, "(l.full_name ILIKE '%' || "+add(s)+" || '%' OR l.phone LIKE '%' || regexp_replace($"+strconv.Itoa(len(args))+", '\\D', '', 'g') || '%')")
		}
		if s := q.Get("source"); s != "" {
			conds = append(conds, "l.source = "+add(s))
		}
		if q.Get("no_contact") == "1" {
			conds = append(conds, "l.stage='new' AND l.last_contact_at IS NULL AND l.created_at < NOW() - interval '24 hours'")
		}
		cf, err := customfield.Load(r.Context(), tx, "lead", false)
		if err != nil {
			return err
		}
		cfConds, err := customfield.FilterSQL(cf, q, "l.custom_fields", &args, p)
		if err != nil {
			return err
		}
		conds = append(conds, cfConds...)
		base := ""
		if len(conds) > 0 {
			base = " WHERE " + strings.Join(conds, " AND ")
		}
		crow, err := tx.Query(r.Context(), "SELECT l.stage, count(*) FROM leads l"+base+" GROUP BY l.stage", args...)
		if err != nil {
			return err
		}
		for crow.Next() {
			var s string
			var c int
			crow.Scan(&s, &c)
			counts[s] = c
		}
		crow.Close()
		where := base
		if st := q.Get("stage"); st != "" {
			args = append(args, strings.Split(st, ","))
			c := "l.stage = ANY($" + strconv.Itoa(len(args)) + "::text[])"
			if where == "" {
				where = " WHERE " + c
			} else {
				where += " AND " + c
			}
		}
		rows, err := tx.Query(r.Context(), sel+where+" ORDER BY l.created_at DESC LIMIT 500", args...)
		if err != nil {
			return err
		}
		out, err = scan(rows)
		for i := range out {
			out[i].CF = customfield.Strip(cf, out[i].CF, p, true)
		}
		return err
	})
	if err != nil {
		return err
	}
	return httpx.OKMeta(w, out, map[string]any{"counts": counts})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) error {
	p := auth.FromContext(r.Context())
	out := map[string]any{}
	err := h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), sel+" WHERE l.id=$1", chi.URLParam(r, "id"))
		if err != nil {
			return err
		}
		list, err := scan(rows)
		if err != nil {
			return err
		}
		if len(list) == 0 {
			return httpx.NotFound("LEAD_NOT_FOUND")
		}
		fields, _ := customfield.Load(r.Context(), tx, "lead", false)
		list[0].CF = customfield.Strip(fields, list[0].CF, p, false)
		out["lead"] = list[0]
		out["fields"] = customfield.Visible(fields, p)
		hrows, err := tx.Query(r.Context(), `SELECT a.action, a.new_values, a.created_at, coalesce(u.full_name,'Система') FROM audit_logs a LEFT JOIN users u ON u.id=a.user_id
			WHERE a.entity_type='lead' AND a.entity_id=$1 ORDER BY a.created_at DESC LIMIT 30`, list[0].ID)
		if err != nil {
			return err
		}
		hist := []map[string]any{}
		for hrows.Next() {
			var action, user string
			var nv map[string]any
			var at time.Time
			hrows.Scan(&action, &nv, &at, &user)
			hist = append(hist, map[string]any{"action": action, "new": nv, "created_at": at, "user": user})
		}
		hrows.Close()
		out["history"] = hist
		trows, err := tx.Query(r.Context(), `SELECT l.id, l.starts_at, g.name, a.status FROM lesson_guests lg JOIN lessons l ON l.id=lg.lesson_id JOIN groups g ON g.id=l.group_id
			LEFT JOIN attendance a ON a.lesson_id=l.id AND a.lead_id=lg.lead_id WHERE lg.lead_id=$1 ORDER BY l.starts_at`, list[0].ID)
		if err != nil {
			return err
		}
		trials := []map[string]any{}
		for trows.Next() {
			var lid, gname string
			var at time.Time
			var st *string
			trows.Scan(&lid, &at, &gname, &st)
			trials = append(trials, map[string]any{"lesson_id": lid, "starts_at": at, "group": gname, "status": st})
		}
		trows.Close()
		out["trials"] = trials
		return nil
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, out)
}

type input struct {
	BranchID      *string        `json:"branch_id"`
	FullName      *string        `json:"full_name"`
	ContactName   *string        `json:"contact_name"`
	Phone         *string        `json:"phone"`
	Email         *string        `json:"email"`
	Telegram      *string        `json:"telegram"`
	CourseID      *string        `json:"course_id"`
	PreferredTime *string        `json:"preferred_time"`
	AgeOrGrade    *string        `json:"age_or_grade"`
	Source        *string        `json:"source"`
	UTM           map[string]any `json:"utm"`
	ResponsibleID *string        `json:"responsible_id"`
	Stage         *string        `json:"stage"`
	LostReason    *string        `json:"lost_reason"`
	Comment       *string        `json:"comment"`
	Contacted     bool           `json:"contacted"`
	CF            map[string]any `json:"custom_fields"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) error {
	var in input
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	p := auth.FromContext(r.Context())
	id, dups, err := CreateLead(r, h.DB, in, p)
	if err != nil {
		return err
	}
	return httpx.Created(w, map[string]any{"id": id, "duplicates": dups})
}

// CreateLead используется и формой в CRM, и публичным приёмом заявок.
func CreateLead(r *http.Request, db *database.DB, in input, p *auth.Principal) (string, []map[string]string, error) {
	v := &httpx.Validation{}
	if in.FullName == nil || strings.TrimSpace(*in.FullName) == "" {
		v.Add("full_name", "REQUIRED")
	}
	phone := ""
	if in.Phone != nil {
		var ok bool
		phone, ok = validate.Phone(*in.Phone)
		if !ok {
			v.Add("phone", "INVALID_FORMAT")
		}
	}
	if phone == "" {
		v.Add("phone", "REQUIRED")
	}
	if err := v.Err(); err != nil {
		return "", nil, err
	}
	ctx := r.Context()
	var id string
	dups := []map[string]string{}
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT 'student', id::text, full_name FROM students WHERE phone=$1 UNION ALL SELECT 'lead', id::text, full_name FROM leads WHERE phone=$1 AND stage NOT IN ('won','lost')`, phone)
		if err != nil {
			return err
		}
		for rows.Next() {
			var k, i, n string
			rows.Scan(&k, &i, &n)
			dups = append(dups, map[string]string{"kind": k, "id": i, "full_name": n})
		}
		rows.Close()
		fields, err := customfield.Load(ctx, tx, "lead", false)
		if err != nil {
			return err
		}
		cf, err := customfield.Apply(ctx, tx, fields, nil, in.CF, p, customfield.Context{Table: "leads", Creating: true})
		if err != nil {
			return err
		}
		cfb, _ := json.Marshal(cf)
		utm, _ := json.Marshal(orEmpty(in.UTM))
		resp := deref(in.ResponsibleID)
		if resp == "" && p != nil && p.HasRole("sales") {
			resp = p.UserID
		}
		if err := tx.QueryRow(ctx, `INSERT INTO leads (tenant_id, branch_id, full_name, contact_name, phone, email, telegram, course_id, preferred_time, age_or_grade, source, utm, responsible_id, comment, custom_fields)
			VALUES (app_current_tenant(), NULLIF($1,'')::uuid, $2, $3, $4, $5, $6, NULLIF($7,'')::uuid, $8, $9, $10, $11, NULLIF($12,'')::uuid, $13, $14) RETURNING id`,
			deref(in.BranchID), strings.TrimSpace(*in.FullName), nz(in.ContactName), phone, nz(in.Email), nz(in.Telegram), deref(in.CourseID),
			nz(in.PreferredTime), nz(in.AgeOrGrade), nz(in.Source), utm, resp, nz(in.Comment), cfb).Scan(&id); err != nil {
			return err
		}
		if err := database.Publish(ctx, tx, "lead.created", map[string]any{"lead_id": id}); err != nil {
			return err
		}
		return database.Audit(ctx, tx, "lead", id, "create", nil, map[string]any{"source": in.Source})
	})
	return id, dups, err
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) error {
	var in input
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	id := chi.URLParam(r, "id")
	p := auth.FromContext(r.Context())
	ctx := r.Context()
	return h.DB.Tx(ctx, func(tx pgx.Tx) error {
		var stage string
		var cur map[string]any
		if err := tx.QueryRow(ctx, `SELECT stage, custom_fields FROM leads WHERE id=$1 FOR UPDATE`, id).Scan(&stage, &cur); err != nil {
			return err
		}
		if in.Stage != nil {
			if !slices.Contains(Stages, *in.Stage) {
				return (&httpx.Validation{Fields: []httpx.FieldError{{Field: "stage", Code: "INVALID"}}}).Err()
			}
			if *in.Stage == "lost" && strings.TrimSpace(deref(in.LostReason)) == "" {
				return (&httpx.Validation{Fields: []httpx.FieldError{{Field: "lost_reason", Code: "REQUIRED"}}}).Err()
			}
			if *in.Stage == "won" {
				return httpx.BadRequest("USE_CONVERT", "Чтобы перевести лида в «Оплатил», конвертируйте его в ученика")
			}
		}
		if in.CF != nil {
			fields, err := customfield.Load(ctx, tx, "lead", false)
			if err != nil {
				return err
			}
			cf, err := customfield.Apply(ctx, tx, fields, cur, in.CF, p, customfield.Context{Table: "leads", RecordID: id})
			if err != nil {
				return err
			}
			b, _ := json.Marshal(cf)
			tx.Exec(ctx, `UPDATE leads SET custom_fields=$2 WHERE id=$1`, id, b)
		}
		if in.Phone != nil {
			ph, ok := validate.Phone(*in.Phone)
			if !ok || ph == "" {
				return (&httpx.Validation{Fields: []httpx.FieldError{{Field: "phone", Code: "INVALID_FORMAT"}}}).Err()
			}
			in.Phone = &ph
		}
		if _, err := tx.Exec(ctx, `UPDATE leads SET full_name=COALESCE($2, full_name), contact_name=COALESCE($3, contact_name), phone=COALESCE($4, phone),
			email=COALESCE($5, email), telegram=COALESCE($6, telegram), course_id=CASE WHEN $7::text IS NULL THEN course_id ELSE NULLIF($7,'')::uuid END,
			preferred_time=COALESCE($8, preferred_time), age_or_grade=COALESCE($9, age_or_grade), source=COALESCE($10, source),
			responsible_id=CASE WHEN $11::text IS NULL THEN responsible_id ELSE NULLIF($11,'')::uuid END, stage=COALESCE($12, stage),
			lost_reason=CASE WHEN $12='lost' THEN $13 ELSE lost_reason END, comment=COALESCE($14, comment),
			branch_id=CASE WHEN $15::text IS NULL THEN branch_id ELSE NULLIF($15,'')::uuid END,
			last_contact_at=CASE WHEN $16 OR ($12 IS NOT NULL AND $12 <> 'new') THEN NOW() ELSE last_contact_at END, updated_at=NOW() WHERE id=$1`,
			id, in.FullName, in.ContactName, in.Phone, in.Email, in.Telegram, in.CourseID, in.PreferredTime, in.AgeOrGrade, in.Source, in.ResponsibleID,
			in.Stage, in.LostReason, in.Comment, in.BranchID, in.Contacted); err != nil {
			return err
		}
		if in.Stage != nil && *in.Stage != stage {
			database.Audit(ctx, tx, "lead", id, "stage_changed", map[string]any{"stage": stage}, map[string]any{"stage": *in.Stage, "lost_reason": in.LostReason})
		} else {
			database.Audit(ctx, tx, "lead", id, "update", nil, in)
		}
		return nil
	})
}

// convert — конвертация лида в ученика одним действием: ученик, родитель, запись в группу, абонемент, первый платёж (ТЗ 5.4).
func (h *Handler) convert(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		BranchID     string         `json:"branch_id"`
		StudentName  string         `json:"student_name"`
		StudentPhone string         `json:"student_phone"`
		ParentName   string         `json:"parent_name"`
		ParentPhone  string         `json:"parent_phone"`
		Relation     string         `json:"relation"`
		GroupID      string         `json:"group_id"`
		CF           map[string]any `json:"custom_fields"`
		Subscription *struct {
			Type         string `json:"type"`
			LessonsTotal *int   `json:"lessons_total"`
			PriceTiyn    int64  `json:"price_tiyn"`
			StartsOn     string `json:"starts_on"`
		} `json:"subscription"`
		Payment *struct {
			AmountTiyn int64  `json:"amount_tiyn"`
			Method     string `json:"method"`
		} `json:"payment"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	id := chi.URLParam(r, "id")
	p := auth.FromContext(r.Context())
	if req.Payment != nil && !p.Can("payments.create") {
		return httpx.Forbidden()
	}
	ctx := r.Context()
	var studentID string
	err := h.DB.Tx(ctx, func(tx pgx.Tx) error {
		var l dto
		rows, err := tx.Query(ctx, sel+" WHERE l.id=$1 FOR UPDATE OF l", id)
		if err != nil {
			return err
		}
		list, err := scan(rows)
		if err != nil || len(list) == 0 {
			return httpx.NotFound("LEAD_NOT_FOUND")
		}
		l = list[0]
		if l.StudentID != nil {
			return httpx.Conflict("LEAD_CONVERTED", "Лид уже конвертирован в ученика")
		}
		branch := firstNonEmpty(req.BranchID, deref(l.BranchID))
		if branch == "" {
			tx.QueryRow(ctx, `SELECT id FROM branches WHERE is_active ORDER BY created_at LIMIT 1`).Scan(&branch)
		}
		if !p.CanBranch(branch) {
			return httpx.Forbidden()
		}
		name := firstNonEmpty(strings.TrimSpace(req.StudentName), l.FullName)
		sphone, _ := validate.Phone(req.StudentPhone)
		// пользовательские поля ученика: из запроса; совпадающие по ключу поля лида переносятся
		sfields, err := customfield.Load(ctx, tx, "student", false)
		if err != nil {
			return err
		}
		patch := map[string]any{}
		sk := customfield.ByKey(sfields)
		lfields, _ := customfield.Load(ctx, tx, "lead", false)
		lk := customfield.ByKey(lfields)
		for k, v := range l.CF {
			if sf, ok := sk[k]; ok && lk[k] != nil && sf.Type == lk[k].Type && sf.Type != "select" && sf.Type != "multiselect" {
				patch[k] = v
			}
		}
		for k, v := range req.CF {
			patch[k] = v
		}
		var courseIDs []string
		if req.GroupID != "" {
			var c string
			tx.QueryRow(ctx, `SELECT course_id FROM groups WHERE id=$1`, req.GroupID).Scan(&c)
			courseIDs = []string{c}
		}
		cf, err := customfield.Apply(ctx, tx, sfields, nil, patch, p, customfield.Context{Table: "students", Status: "active", CourseIDs: courseIDs, BranchID: branch, Creating: true})
		if err != nil {
			return err
		}
		cfb, _ := json.Marshal(cf)
		if err := tx.QueryRow(ctx, `INSERT INTO students (tenant_id, branch_id, full_name, phone, email, source, responsible_id, status, lead_id, custom_fields, started_at)
			VALUES (app_current_tenant(), $1, $2, NULLIF($3,''), $4, $5, $6, 'active', $7, $8, CURRENT_DATE) RETURNING id`,
			branch, name, sphone, l.Email, l.Source, l.ResponsibleID, id, cfb).Scan(&studentID); err != nil {
			return err
		}
		tx.Exec(ctx, `INSERT INTO student_balances (tenant_id, student_id) VALUES (app_current_tenant(), $1)`, studentID)
		// родитель: из запроса или контактное лицо лида с его телефоном
		pname := firstNonEmpty(strings.TrimSpace(req.ParentName), deref(l.ContactName))
		pphone := firstNonEmpty(req.ParentPhone, l.Phone)
		if pname != "" {
			ph, _ := validate.Phone(pphone)
			var pid string
			if ph != "" {
				tx.QueryRow(ctx, `SELECT id FROM parents WHERE phone=$1 LIMIT 1`, ph).Scan(&pid)
			}
			if pid == "" {
				if err := tx.QueryRow(ctx, `INSERT INTO parents (tenant_id, full_name, phone, whatsapp, telegram_link_code) VALUES (app_current_tenant(), $1, NULLIF($2,''), NULLIF($2,''), $3) RETURNING id`,
					pname, ph, auth.RandomCode(16)).Scan(&pid); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(ctx, `INSERT INTO student_parents (tenant_id, student_id, parent_id, relation, is_payer, is_legal_rep) VALUES (app_current_tenant(), $1, $2, $3, true, true)
				ON CONFLICT DO NOTHING`, studentID, pid, firstNonEmpty(req.Relation, "parent")); err != nil {
				return err
			}
		}
		if req.GroupID != "" {
			if _, err := tx.Exec(ctx, `INSERT INTO group_students (tenant_id, group_id, student_id) VALUES (app_current_tenant(), $1, $2)`, req.GroupID, studentID); err != nil {
				return err
			}
		}
		if s := req.Subscription; s != nil && s.Type != "" {
			start := firstNonEmpty(s.StartsOn, time.Now().Format("2006-01-02"))
			var subID string
			var cname string
			if len(courseIDs) > 0 {
				tx.QueryRow(ctx, `SELECT name FROM courses WHERE id=$1`, courseIDs[0]).Scan(&cname)
			}
			title := "Абонемент"
			if s.LessonsTotal != nil {
				title = strconv.Itoa(*s.LessonsTotal) + " занятий"
			} else if s.Type == "monthly" {
				title = "Месяц"
			}
			if cname != "" {
				title += " · " + cname
			}
			var courseID *string
			if len(courseIDs) > 0 {
				courseID = &courseIDs[0]
			}
			if err := tx.QueryRow(ctx, `INSERT INTO subscriptions (tenant_id, student_id, course_id, type, name, price_tiyn, lessons_total, starts_on, ends_on, created_by)
				VALUES (app_current_tenant(), $1, $2, $3, $4, $5, $6, $7::date, CASE WHEN $3='monthly' THEN $7::date + interval '1 month' - interval '1 day' ELSE $7::date + 45 END, $8) RETURNING id`,
				studentID, courseID, s.Type, title, s.PriceTiyn, s.LessonsTotal, start, p.UserID).Scan(&subID); err != nil {
				return err
			}
			if s.PriceTiyn > 0 {
				if err := finance.Post(ctx, tx, studentID, "charge", -s.PriceTiyn, "subscription", &subID, "Абонемент: "+title, p.UserID); err != nil {
					return err
				}
			}
		}
		if pay := req.Payment; pay != nil && pay.AmountTiyn > 0 {
			var payID string
			if err := tx.QueryRow(ctx, `INSERT INTO payments (tenant_id, student_id, amount_tiyn, method, received_by) VALUES (app_current_tenant(), $1, $2, $3, $4) RETURNING id`,
				studentID, pay.AmountTiyn, firstNonEmpty(pay.Method, "cash"), p.UserID).Scan(&payID); err != nil {
				return err
			}
			if err := finance.Post(ctx, tx, studentID, "payment", pay.AmountTiyn, "payment", &payID, "Первый платёж", p.UserID); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE leads SET stage='won', student_id=$2, updated_at=NOW() WHERE id=$1`, id, studentID); err != nil {
			return err
		}
		database.Audit(ctx, tx, "lead", id, "converted", nil, map[string]any{"student_id": studentID})
		database.Audit(ctx, tx, "student", studentID, "create", nil, map[string]any{"from_lead": id, "full_name": name, "status": "active"})
		return database.Publish(ctx, tx, "lead.converted", map[string]any{"lead_id": id, "student_id": studentID})
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]string{"student_id": studentID})
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func nz(s *string) *string {
	if s == nil {
		return nil
	}
	return httpx.Ptr(*s)
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func orEmpty(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}
