// Пакет employee — преподаватели и сотрудники, пользователи организации и роли.
package employee

import (
	"encoding/json"
	"net/http"
	"slices"
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

type Handler struct{ DB *database.DB }

func (h *Handler) Routes(r chi.Router) {
	// список преподавателей нужен для расписания и групп — доступен с правом на группы или расписание
	r.With(mw.Perm("employees.view", "groups.view", "schedule.view", "leads.view")).Get("/employees", httpx.Handler(h.list).ServeHTTP)
	r.With(mw.Perm("employees.view")).Get("/employees/{id}", httpx.Handler(h.get).ServeHTTP)
	r.With(mw.Perm("employees.edit")).Post("/employees", httpx.Handler(h.create).ServeHTTP)
	r.With(mw.Perm("employees.edit")).Patch("/employees/{id}", httpx.Handler(h.update).ServeHTTP)

	r.With(mw.Perm("roles.manage", "employees.edit")).Get("/users", httpx.Handler(h.users).ServeHTTP)
	r.With(mw.Perm("roles.manage")).Patch("/users/{id}", httpx.Handler(h.updateUser).ServeHTTP)
	r.With(mw.Perm("roles.manage", "employees.edit")).Get("/roles", httpx.Handler(h.roles).ServeHTTP)
	r.With(mw.Perm("roles.manage")).Post("/roles", httpx.Handler(h.createRole).ServeHTTP)
	r.With(mw.Perm("roles.manage")).Patch("/roles/{id}", httpx.Handler(h.updateRole).ServeHTTP)
	r.Get("/permissions", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error { return httpx.OK(w, auth.AllPermissions) }).ServeHTTP)
}

type dto struct {
	ID          string         `json:"id"`
	UserID      *string        `json:"user_id"`
	FullName    string         `json:"full_name"`
	Phone       *string        `json:"phone"`
	Email       *string        `json:"email"`
	Position    *string        `json:"position"`
	IsTeacher   bool           `json:"is_teacher"`
	Subjects    []string       `json:"subjects"`
	BranchIDs   []string       `json:"branch_ids"`
	HiredAt     *time.Time     `json:"hired_at"`
	Status      string         `json:"status"`
	PayType     string         `json:"pay_type"`
	PayRate     *int64         `json:"pay_rate_tiyn,omitempty"`
	Groups      int            `json:"groups_count"`
	WeekLessons int            `json:"week_lessons"`
	CF          map[string]any `json:"custom_fields"`
	Roles       []string       `json:"roles"`
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) error {
	p := auth.FromContext(r.Context())
	teachersOnly := r.URL.Query().Get("teachers") == "1"
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "active"
	}
	out := []dto{}
	err := h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `SELECT e.id, e.user_id, e.full_name, e.phone, e.email, e.position, e.is_teacher, e.subjects, e.branch_ids::text[], e.hired_at, e.status,
			e.pay_type, e.pay_rate_tiyn, e.custom_fields,
			(SELECT count(*) FROM groups g WHERE g.teacher_id=e.id AND g.status='active'),
			(SELECT count(*) FROM lessons l WHERE l.teacher_id=e.id AND l.status<>'cancelled' AND l.starts_at >= date_trunc('week', NOW()) AND l.starts_at < date_trunc('week', NOW()) + interval '7 days'),
			coalesce((SELECT array_agg(r.code) FROM user_roles ur JOIN roles r ON r.id=ur.role_id WHERE ur.user_id=e.user_id), '{}')
			FROM employees e WHERE ($1 = 'all' OR e.status=$1) AND (NOT $2 OR e.is_teacher) ORDER BY e.full_name`, status, teachersOnly)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var d dto
			var rate int64
			if err := rows.Scan(&d.ID, &d.UserID, &d.FullName, &d.Phone, &d.Email, &d.Position, &d.IsTeacher, &d.Subjects, &d.BranchIDs, &d.HiredAt, &d.Status,
				&d.PayType, &rate, &d.CF, &d.Groups, &d.WeekLessons, &d.Roles); err != nil {
				return err
			}
			if p.Can("employees.edit") || p.Can("finance.view") {
				d.PayRate = &rate
			}
			if bf := p.BranchFilter(); bf != nil && len(d.BranchIDs) > 0 {
				ok := false
				for _, b := range d.BranchIDs {
					ok = ok || slices.Contains(bf, b)
				}
				if !ok {
					continue
				}
			}
			out = append(out, d)
		}
		return rows.Err()
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, out)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	out := map[string]any{}
	err := h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		var d dto
		var rate int64
		if err := tx.QueryRow(r.Context(), `SELECT e.id, e.user_id, e.full_name, e.phone, e.email, e.position, e.is_teacher, e.subjects, e.branch_ids::text[], e.hired_at, e.status, e.pay_type, e.pay_rate_tiyn, e.custom_fields
			FROM employees e WHERE id=$1`, id).Scan(&d.ID, &d.UserID, &d.FullName, &d.Phone, &d.Email, &d.Position, &d.IsTeacher, &d.Subjects, &d.BranchIDs, &d.HiredAt, &d.Status, &d.PayType, &rate, &d.CF); err != nil {
			return err
		}
		d.PayRate = &rate
		out["employee"] = d
		rows, err := tx.Query(r.Context(), `SELECT g.id, g.name, c.name, (SELECT count(*) FROM group_students gs WHERE gs.group_id=g.id AND gs.left_at IS NULL)
			FROM groups g JOIN courses c ON c.id=g.course_id WHERE g.teacher_id=$1 AND g.status='active' ORDER BY g.name`, id)
		if err != nil {
			return err
		}
		groups := []map[string]any{}
		for rows.Next() {
			var gid, gn, cn string
			var cnt int
			rows.Scan(&gid, &gn, &cn, &cnt)
			groups = append(groups, map[string]any{"id": gid, "name": gn, "course": cn, "students": cnt})
		}
		rows.Close()
		out["groups"] = groups
		var done, cancelled, unmarked int
		tx.QueryRow(r.Context(), `SELECT count(*) FILTER (WHERE status='done'), count(*) FILTER (WHERE status='cancelled'),
			count(*) FILTER (WHERE status='planned' AND ends_at < NOW())
			FROM lessons WHERE teacher_id=$1 AND starts_at >= date_trunc('month', NOW())`, id).Scan(&done, &cancelled, &unmarked)
		out["month"] = map[string]int{"done": done, "cancelled": cancelled, "unmarked": unmarked}
		return nil
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, out)
}

type input struct {
	FullName  *string        `json:"full_name"`
	Phone     *string        `json:"phone"`
	Email     *string        `json:"email"`
	Position  *string        `json:"position"`
	IsTeacher *bool          `json:"is_teacher"`
	Subjects  []string       `json:"subjects"`
	BranchIDs []string       `json:"branch_ids"`
	HiredAt   *string        `json:"hired_at"`
	Status    *string        `json:"status"`
	PayType   *string        `json:"pay_type"`
	PayRate   *int64         `json:"pay_rate_tiyn"`
	CF        map[string]any `json:"custom_fields"`
	// доступ в систему
	CreateAccess bool     `json:"create_access"`
	Password     string   `json:"password"`
	Roles        []string `json:"roles"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) error {
	var in input
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	v := &httpx.Validation{}
	if in.FullName == nil || strings.TrimSpace(*in.FullName) == "" {
		v.Add("full_name", "REQUIRED")
	}
	phone := ""
	if in.Phone != nil {
		var ok bool
		if phone, ok = validate.Phone(*in.Phone); !ok {
			v.Add("phone", "INVALID_FORMAT")
		}
	}
	email := strings.ToLower(strings.TrimSpace(deref(in.Email)))
	if !validate.Email(email) {
		v.Add("email", "INVALID_FORMAT")
	}
	if in.CreateAccess {
		if email == "" {
			v.Add("email", "REQUIRED")
		}
		if len([]rune(in.Password)) < 10 {
			v.Add("password", "TOO_SHORT")
		}
	}
	if err := v.Err(); err != nil {
		return err
	}
	p := auth.FromContext(r.Context())
	ctx := r.Context()
	var id string
	err := h.DB.Tx(ctx, func(tx pgx.Tx) error {
		var userID *string
		if in.CreateAccess {
			// email уникален глобально — проверка под системной ролью
			var exists bool
			h.DB.Sys.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE lower(email)=$1)`, email).Scan(&exists)
			if exists {
				return httpx.Conflict("EMAIL_TAKEN", "Пользователь с таким email уже существует")
			}
			hash, err := auth.HashPassword(in.Password)
			if err != nil {
				return err
			}
			var uid string
			if err := tx.QueryRow(ctx, `INSERT INTO users (tenant_id, email, phone, full_name, password_hash) VALUES (app_current_tenant(), $1, NULLIF($2,''), $3, $4) RETURNING id`,
				email, phone, strings.TrimSpace(*in.FullName), hash).Scan(&uid); err != nil {
				return err
			}
			userID = &uid
			roles := in.Roles
			if len(roles) == 0 {
				roles = []string{"teacher"}
			}
			if err := setRoles(r, tx, uid, roles, in.BranchIDs, p); err != nil {
				return err
			}
		}
		isTeacher := in.IsTeacher == nil || *in.IsTeacher
		payType := "per_lesson"
		if in.PayType != nil {
			payType = *in.PayType
		}
		var rate int64
		if in.PayRate != nil {
			rate = *in.PayRate
		}
		cf, _ := json.Marshal(orEmpty(in.CF))
		if err := tx.QueryRow(ctx, `INSERT INTO employees (tenant_id, user_id, full_name, phone, email, position, is_teacher, subjects, branch_ids, hired_at, pay_type, pay_rate_tiyn, custom_fields)
			VALUES (app_current_tenant(), $1, $2, NULLIF($3,''), NULLIF($4,''), $5, $6, $7, $8::uuid[], NULLIF($9,'')::date, $10, $11, $12) RETURNING id`,
			userID, strings.TrimSpace(*in.FullName), phone, email, httpx.Ptr(deref(in.Position)), isTeacher, orStrings(in.Subjects), orStrings(in.BranchIDs),
			deref(in.HiredAt), payType, rate, cf).Scan(&id); err != nil {
			return err
		}
		return database.Audit(ctx, tx, "employee", id, "create", nil, map[string]any{"full_name": *in.FullName, "access": in.CreateAccess, "roles": in.Roles})
	})
	if err != nil {
		return err
	}
	return httpx.Created(w, map[string]string{"id": id})
}

func setRoles(r *http.Request, tx pgx.Tx, userID string, roles, branchIDs []string, p *auth.Principal) error {
	ctx := r.Context()
	if slices.Contains(roles, "owner") && !p.HasRole("owner") {
		return httpx.NewError(403, "OWNER_ONLY", "Назначить роль владельца может только владелец")
	}
	if _, err := tx.Exec(ctx, `DELETE FROM user_roles WHERE user_id=$1`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO user_roles (tenant_id, user_id, role_id) SELECT app_current_tenant(), $1, id FROM roles WHERE code = ANY($2)`, userID, roles); err != nil {
		return err
	}
	if branchIDs != nil {
		tx.Exec(ctx, `DELETE FROM user_branch_access WHERE user_id=$1`, userID)
		for _, b := range branchIDs {
			if _, err := tx.Exec(ctx, `INSERT INTO user_branch_access (tenant_id, user_id, branch_id) VALUES (app_current_tenant(), $1, $2)`, userID, b); err != nil {
				return err
			}
		}
	}
	return database.Audit(ctx, tx, "user", userID, "roles_changed", nil, map[string]any{"roles": roles, "branches": branchIDs})
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) error {
	var in input
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	return h.DB.Tx(ctx, func(tx pgx.Tx) error {
		set := func(col string, val any) error {
			_, err := tx.Exec(ctx, "UPDATE employees SET "+col+" WHERE id=$1", id, val)
			return err
		}
		var errs []error
		if in.FullName != nil {
			errs = append(errs, set("full_name=$2", strings.TrimSpace(*in.FullName)))
		}
		if in.Phone != nil {
			ph, ok := validate.Phone(*in.Phone)
			if !ok {
				return (&httpx.Validation{Fields: []httpx.FieldError{{Field: "phone", Code: "INVALID_FORMAT"}}}).Err()
			}
			errs = append(errs, set("phone=NULLIF($2,'')", ph))
		}
		if in.Email != nil {
			errs = append(errs, set("email=NULLIF($2,'')", strings.TrimSpace(*in.Email)))
		}
		if in.Position != nil {
			errs = append(errs, set("position=NULLIF($2,'')", *in.Position))
		}
		if in.IsTeacher != nil {
			errs = append(errs, set("is_teacher=$2", *in.IsTeacher))
		}
		if in.Subjects != nil {
			errs = append(errs, set("subjects=$2", in.Subjects))
		}
		if in.BranchIDs != nil {
			errs = append(errs, set("branch_ids=$2::uuid[]", in.BranchIDs))
		}
		if in.Status != nil {
			errs = append(errs, set("status=$2", *in.Status))
			if *in.Status == "archived" {
				tx.Exec(ctx, `UPDATE users SET status='blocked' WHERE id=(SELECT user_id FROM employees WHERE id=$1)`, id)
			}
		}
		if in.PayType != nil {
			errs = append(errs, set("pay_type=$2", *in.PayType))
		}
		if in.PayRate != nil {
			errs = append(errs, set("pay_rate_tiyn=$2", *in.PayRate))
		}
		if in.CF != nil {
			b, _ := json.Marshal(in.CF)
			errs = append(errs, set("custom_fields=$2", b))
		}
		for _, e := range errs {
			if e != nil {
				return e
			}
		}
		return database.Audit(ctx, tx, "employee", id, "update", nil, in)
	})
}

// ---------------------------------------------------------------- пользователи и роли

func (h *Handler) users(w http.ResponseWriter, r *http.Request) error {
	out := []map[string]any{}
	err := h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `SELECT u.id, u.full_name, u.email, u.status, u.last_login_at,
			coalesce((SELECT array_agg(r.code) FROM user_roles ur JOIN roles r ON r.id=ur.role_id WHERE ur.user_id=u.id), '{}'),
			coalesce((SELECT array_agg(branch_id::text) FROM user_branch_access WHERE user_id=u.id), '{}')
			FROM users u ORDER BY u.full_name`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id, name, email, status string
			var last *time.Time
			var roles, branches []string
			rows.Scan(&id, &name, &email, &status, &last, &roles, &branches)
			out = append(out, map[string]any{"id": id, "full_name": name, "email": email, "status": status, "last_login_at": last, "roles": roles, "branch_ids": branches})
		}
		return rows.Err()
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, out)
}

func (h *Handler) updateUser(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Roles     []string `json:"roles"`
		BranchIDs []string `json:"branch_ids"`
		Status    *string  `json:"status"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	id := chi.URLParam(r, "id")
	p := auth.FromContext(r.Context())
	return h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		if req.Roles != nil {
			if err := setRoles(r, tx, id, req.Roles, req.BranchIDs, p); err != nil {
				return err
			}
		}
		if req.Status != nil {
			if id == p.UserID {
				return httpx.BadRequest("SELF_BLOCK", "Нельзя заблокировать самого себя")
			}
			tx.Exec(r.Context(), `UPDATE users SET status=$2 WHERE id=$1`, id, *req.Status)
			if *req.Status != "active" {
				h.DB.Sys.Exec(r.Context(), `UPDATE sessions SET revoked_at=NOW() WHERE user_id=$1 AND revoked_at IS NULL`, id)
			}
			database.Audit(r.Context(), tx, "user", id, "status_changed", nil, map[string]any{"status": *req.Status})
		}
		return nil
	})
}

func (h *Handler) roles(w http.ResponseWriter, r *http.Request) error {
	out := []map[string]any{}
	err := h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `SELECT r.id, r.code, r.name, r.is_system, r.all_branches, r.permissions, (SELECT count(*) FROM user_roles ur WHERE ur.role_id=r.id)
			FROM roles r ORDER BY r.is_system DESC, r.created_at`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id, code, name string
			var sys, all bool
			var perms []string
			var users int
			rows.Scan(&id, &code, &name, &sys, &all, &perms, &users)
			out = append(out, map[string]any{"id": id, "code": code, "name": name, "is_system": sys, "all_branches": all, "permissions": perms, "users": users})
		}
		return rows.Err()
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, out)
}

// createRole — пользовательская роль на основе существующей (ТЗ 2.9). Системные роли нельзя удалить, но можно скопировать.
func (h *Handler) createRole(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Name        string   `json:"name"`
		BaseRoleID  string   `json:"base_role_id"`
		Permissions []string `json:"permissions"`
		AllBranches bool     `json:"all_branches"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if strings.TrimSpace(req.Name) == "" {
		return httpx.BadRequest("VALIDATION_FAILED", "Укажите название роли")
	}
	var id string
	err := h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		perms := filterPerms(req.Permissions)
		if req.BaseRoleID != "" && req.Permissions == nil {
			tx.QueryRow(r.Context(), `SELECT permissions, all_branches FROM roles WHERE id=$1`, req.BaseRoleID).Scan(&perms, &req.AllBranches)
		}
		if err := tx.QueryRow(r.Context(), `INSERT INTO roles (tenant_id, code, name, permissions, all_branches) VALUES (app_current_tenant(), $1, $2, $3, $4) RETURNING id`,
			"custom_"+auth.RandomCode(8), strings.TrimSpace(req.Name), perms, req.AllBranches).Scan(&id); err != nil {
			return err
		}
		return database.Audit(r.Context(), tx, "role", id, "create", nil, map[string]any{"name": req.Name, "permissions": perms})
	})
	if err != nil {
		return err
	}
	return httpx.Created(w, map[string]string{"id": id})
}

func (h *Handler) updateRole(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Name        *string  `json:"name"`
		Permissions []string `json:"permissions"`
		AllBranches *bool    `json:"all_branches"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	id := chi.URLParam(r, "id")
	return h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		var code string
		var old []string
		if err := tx.QueryRow(r.Context(), `SELECT code, permissions FROM roles WHERE id=$1`, id).Scan(&code, &old); err != nil {
			return err
		}
		if code == "owner" {
			return httpx.BadRequest("ROLE_LOCKED", "Права владельца не редактируются")
		}
		if req.Name != nil {
			tx.Exec(r.Context(), `UPDATE roles SET name=$2 WHERE id=$1`, id, *req.Name)
		}
		if req.Permissions != nil {
			tx.Exec(r.Context(), `UPDATE roles SET permissions=$2 WHERE id=$1`, id, filterPerms(req.Permissions))
		}
		if req.AllBranches != nil {
			tx.Exec(r.Context(), `UPDATE roles SET all_branches=$2 WHERE id=$1`, id, *req.AllBranches)
		}
		return database.Audit(r.Context(), tx, "role", id, "update", map[string]any{"permissions": old}, req)
	})
}

func filterPerms(in []string) []string {
	out := []string{}
	for _, p := range in {
		if slices.Contains(auth.AllPermissions, p) && !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func orStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func orEmpty(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

var _ = customfield.Masked
