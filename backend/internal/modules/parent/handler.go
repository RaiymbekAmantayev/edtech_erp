package parent

import (
	"context"
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
	"emp/internal/platform/telegram"
	"emp/internal/platform/validate"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

type Handler struct {
	DB      *database.DB
	BotName string
}

func (h *Handler) Routes(r chi.Router) {
	r.With(mw.Perm("parents.view")).Get("/parents", httpx.Handler(h.list).ServeHTTP)
	r.With(mw.Perm("parents.view")).Get("/parents/{id}", httpx.Handler(h.get).ServeHTTP)
	r.With(mw.Perm("parents.edit")).Post("/parents", httpx.Handler(h.create).ServeHTTP)
	r.With(mw.Perm("parents.edit")).Patch("/parents/{id}", httpx.Handler(h.update).ServeHTTP)
}

type dto struct {
	ID               string          `json:"id"`
	FullName         string          `json:"full_name"`
	Phone            *string         `json:"phone"`
	Email            *string         `json:"email"`
	IIN              *string         `json:"iin"`
	TelegramLinked   bool            `json:"telegram_linked"`
	TelegramLink     string          `json:"telegram_link,omitempty"`
	PreferredChannel string          `json:"preferred_channel"`
	Locale           string          `json:"locale"`
	ConsentAt        *time.Time      `json:"consent_at"`
	Comment          *string         `json:"comment"`
	CustomFields     map[string]any  `json:"custom_fields"`
	Students         json.RawMessage `json:"students"`
}

const sel = `SELECT p.id, p.full_name, p.phone, p.email, p.iin, p.telegram_chat_id IS NOT NULL, p.telegram_link_code, p.preferred_channel, p.locale, p.consent_at, p.comment, p.custom_fields,
	(SELECT coalesce(json_agg(json_build_object('id', s.id, 'full_name', s.full_name, 'relation', sp.relation, 'is_payer', sp.is_payer, 'is_legal_rep', sp.is_legal_rep, 'status', s.status)), '[]')
	 FROM student_parents sp JOIN students s ON s.id = sp.student_id WHERE sp.parent_id = p.id)
	FROM parents p`

// scan: bot — бот школы для ссылок-приглашений; ИИН виден только с правом students.view_iin.
func (h *Handler) scan(rows pgx.Rows, bot string, p *auth.Principal) ([]dto, error) {
	defer rows.Close()
	out := []dto{}
	for rows.Next() {
		var d dto
		var code *string
		if err := rows.Scan(&d.ID, &d.FullName, &d.Phone, &d.Email, &d.IIN, &d.TelegramLinked, &code, &d.PreferredChannel, &d.Locale, &d.ConsentAt, &d.Comment, &d.CustomFields, &d.Students); err != nil {
			return nil, err
		}
		if code != nil && !d.TelegramLinked {
			d.TelegramLink = "https://t.me/" + bot + "?start=" + *code
		}
		if d.IIN != nil && !p.Can("students.view_iin") {
			m := validate.MaskIIN(*d.IIN)
			d.IIN = &m
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) error {
	p := auth.FromContext(r.Context())
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	limit := httpx.QueryInt(r, "limit", 50, 200)
	offset := httpx.QueryInt(r, "offset", 0, 0)
	var out []dto
	var total int
	err := h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		fields, err := customfield.Load(r.Context(), tx, "parent", false)
		if err != nil {
			return err
		}
		args := []any{}
		conds := []string{"p.archived_at IS NULL"}
		if q != "" {
			args = append(args, q)
			conds = append(conds, "(p.full_name ILIKE '%' || $1 || '%' OR p.phone LIKE '%' || regexp_replace($1, '\\D', '', 'g') || '%')")
		}
		if bf := p.BranchFilter(); bf != nil {
			args = append(args, bf)
			conds = append(conds, "EXISTS (SELECT 1 FROM student_parents sp JOIN students s ON s.id=sp.student_id WHERE sp.parent_id=p.id AND s.branch_id = ANY($"+strconv.Itoa(len(args))+"::uuid[]))")
		}
		where := " WHERE " + strings.Join(conds, " AND ")
		tx.QueryRow(r.Context(), "SELECT count(*) FROM parents p"+where, args...).Scan(&total)
		args = append(args, limit, offset)
		bot := telegram.Username(r.Context(), tx, h.BotName)
		rows, err := tx.Query(r.Context(), sel+where+" ORDER BY p.full_name LIMIT $"+strconv.Itoa(len(args)-1)+" OFFSET $"+strconv.Itoa(len(args)), args...)
		if err != nil {
			return err
		}
		out, err = h.scan(rows, bot, p)
		for i := range out {
			out[i].CustomFields = customfield.Strip(fields, out[i].CustomFields, p, true)
		}
		return err
	})
	if err != nil {
		return err
	}
	return httpx.OKMeta(w, out, map[string]any{"total": total})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) error {
	p := auth.FromContext(r.Context())
	var out map[string]any
	err := h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		fields, err := customfield.Load(r.Context(), tx, "parent", false)
		if err != nil {
			return err
		}
		bot := telegram.Username(r.Context(), tx, h.BotName)
		rows, err := tx.Query(r.Context(), sel+" WHERE p.id=$1", chi.URLParam(r, "id"))
		if err != nil {
			return err
		}
		list, err := h.scan(rows, bot, p)
		if err != nil {
			return err
		}
		if len(list) == 0 {
			return httpx.NotFound("PARENT_NOT_FOUND")
		}
		list[0].CustomFields = customfield.Strip(fields, list[0].CustomFields, p, false)
		out = map[string]any{"parent": list[0], "fields": customfield.Visible(fields, p)}
		return nil
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, out)
}

type input struct {
	FullName         *string        `json:"full_name"`
	Phone            *string        `json:"phone"`
	Email            *string        `json:"email"`
	IIN              *string        `json:"iin"`
	PreferredChannel *string        `json:"preferred_channel"`
	Locale           *string        `json:"locale"`
	Comment          *string        `json:"comment"`
	Consent          *bool          `json:"consent"`
	CustomFields     map[string]any `json:"custom_fields"`
}

func (in *input) validate(v *httpx.Validation) {
	if in.FullName != nil && strings.TrimSpace(*in.FullName) == "" {
		v.Add("full_name", "REQUIRED")
	}
	if in.Phone != nil {
		ph, ok := validate.Phone(*in.Phone)
		if !ok {
			v.Add("phone", "INVALID_FORMAT")
		}
		in.Phone = &ph
	}
	if in.Email != nil && !validate.Email(strings.TrimSpace(*in.Email)) {
		v.Add("email", "INVALID_FORMAT")
	}
	if in.IIN != nil {
		iin := strings.TrimSpace(*in.IIN)
		if iin != "" && !validate.IIN(iin) {
			v.Add("iin", "INVALID_IIN")
		}
		in.IIN = &iin
	}
}

// iinTaken — ИИН родителя — логин в кабинет, поэтому он уникален в пределах центра.
func iinTaken(ctx context.Context, tx pgx.Tx, iin *string, exceptID string) error {
	if iin == nil || *iin == "" {
		return nil
	}
	var taken bool
	tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM parents WHERE iin=$1 AND id::text<>$2 AND archived_at IS NULL)`, *iin, exceptID).Scan(&taken)
	if taken {
		return (&httpx.Validation{Fields: []httpx.FieldError{{Field: "iin", Code: "NOT_UNIQUE"}}}).Err()
	}
	return nil
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) error {
	var in input
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	v := &httpx.Validation{}
	if in.FullName == nil {
		v.Add("full_name", "REQUIRED")
	}
	in.validate(v)
	if err := v.Err(); err != nil {
		return err
	}
	p := auth.FromContext(r.Context())
	var id string
	err := h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		fields, err := customfield.Load(r.Context(), tx, "parent", false)
		if err != nil {
			return err
		}
		cf, err := customfield.Apply(r.Context(), tx, fields, nil, in.CustomFields, p, customfield.Context{Table: "parents", Creating: true})
		if err != nil {
			return err
		}
		b, _ := json.Marshal(cf)
		if err := iinTaken(r.Context(), tx, in.IIN, ""); err != nil {
			return err
		}
		var consent *time.Time
		if in.Consent != nil && *in.Consent {
			t := time.Now()
			consent = &t
		}
		if err := tx.QueryRow(r.Context(), `INSERT INTO parents (tenant_id, full_name, phone, whatsapp, email, comment, custom_fields, telegram_link_code, consent_at, consent_method, consent_version, iin)
			VALUES (app_current_tenant(), $1, NULLIF($2,''), NULLIF($2,''), $3, $4, $5, $6, $7, CASE WHEN $7::timestamptz IS NOT NULL THEN 'form' END, CASE WHEN $7::timestamptz IS NOT NULL THEN 'v1' END, NULLIF($8,'')) RETURNING id`,
			strings.TrimSpace(*in.FullName), deref(in.Phone), httpx.Ptr(deref(in.Email)), httpx.Ptr(deref(in.Comment)), b, auth.RandomCode(16), consent, deref(in.IIN)).Scan(&id); err != nil {
			return err
		}
		return database.Audit(r.Context(), tx, "parent", id, "create", nil, map[string]any{"full_name": *in.FullName})
	})
	if err != nil {
		return err
	}
	return httpx.Created(w, map[string]string{"id": id})
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) error {
	var in input
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	v := &httpx.Validation{}
	in.validate(v)
	if err := v.Err(); err != nil {
		return err
	}
	id := chi.URLParam(r, "id")
	p := auth.FromContext(r.Context())
	ctx := r.Context()
	return h.DB.Tx(ctx, func(tx pgx.Tx) error {
		var cur map[string]any
		if err := tx.QueryRow(ctx, `SELECT custom_fields FROM parents WHERE id=$1 FOR UPDATE`, id).Scan(&cur); err != nil {
			return err
		}
		if in.CustomFields != nil {
			fields, err := customfield.Load(ctx, tx, "parent", false)
			if err != nil {
				return err
			}
			cf, err := customfield.Apply(ctx, tx, fields, cur, in.CustomFields, p, customfield.Context{Table: "parents", RecordID: id})
			if err != nil {
				return err
			}
			b, _ := json.Marshal(cf)
			tx.Exec(ctx, `UPDATE parents SET custom_fields=$2 WHERE id=$1`, id, b)
		}
		if in.FullName != nil {
			tx.Exec(ctx, `UPDATE parents SET full_name=$2 WHERE id=$1`, id, strings.TrimSpace(*in.FullName))
		}
		if in.Phone != nil {
			tx.Exec(ctx, `UPDATE parents SET phone=NULLIF($2,'') WHERE id=$1`, id, *in.Phone)
		}
		if in.Email != nil {
			tx.Exec(ctx, `UPDATE parents SET email=NULLIF($2,'') WHERE id=$1`, id, strings.TrimSpace(*in.Email))
		}
		if in.IIN != nil {
			if !p.Can("students.view_iin") {
				return httpx.Forbidden()
			}
			if err := iinTaken(ctx, tx, in.IIN, id); err != nil {
				return err
			}
			// смена ИИН меняет логин кабинета: старая учётная запись родителя закрывается
			if _, err := tx.Exec(ctx, `UPDATE parents SET iin=NULLIF($2,'') WHERE id=$1 AND iin IS DISTINCT FROM NULLIF($2,'')`, id, *in.IIN); err != nil {
				return err
			}
			tx.Exec(ctx, `DELETE FROM portal_accounts WHERE parent_id=$1 AND iin<>$2`, id, *in.IIN)
		}
		if in.PreferredChannel != nil {
			tx.Exec(ctx, `UPDATE parents SET preferred_channel=$2 WHERE id=$1`, id, *in.PreferredChannel)
		}
		if in.Locale != nil {
			tx.Exec(ctx, `UPDATE parents SET locale=$2 WHERE id=$1`, id, *in.Locale)
		}
		if in.Comment != nil {
			tx.Exec(ctx, `UPDATE parents SET comment=NULLIF($2,'') WHERE id=$1`, id, *in.Comment)
		}
		if in.Consent != nil && *in.Consent {
			tx.Exec(ctx, `UPDATE parents SET consent_at=COALESCE(consent_at, NOW()), consent_method=COALESCE(consent_method,'manual'), consent_version=COALESCE(consent_version,'v1') WHERE id=$1`, id)
		}
		audit := in
		if audit.IIN != nil {
			m := validate.MaskIIN(*audit.IIN)
			audit.IIN = &m
		}
		return database.Audit(ctx, tx, "parent", id, "update", nil, audit)
	})
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
