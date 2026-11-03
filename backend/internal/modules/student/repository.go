package student

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"emp/internal/modules/customfield"
	"emp/internal/platform/auth"
	"emp/internal/platform/httpx"
	"emp/internal/platform/validate"

	"github.com/jackc/pgx/v5"
)

type Row struct {
	ID           string          `json:"id"`
	Number       int64           `json:"number"`
	FullName     string          `json:"full_name"`
	Phone        *string         `json:"phone"`
	Email        *string         `json:"email"`
	BirthDate    *time.Time      `json:"birth_date"`
	Status       string          `json:"status"`
	BranchID     string          `json:"branch_id"`
	IIN          *string         `json:"iin"`
	Source       *string         `json:"source"`
	Balance      *int64          `json:"balance_tiyn"`
	Groups       json.RawMessage `json:"groups"`
	Subscription json.RawMessage `json:"subscription"`
	CustomFields map[string]any  `json:"custom_fields"`
	ArchivedAt   *time.Time      `json:"archived_at"`
	CreatedAt    time.Time       `json:"created_at"`
}

const listSelect = `SELECT s.id, s.number, s.full_name, s.phone, s.email, s.birth_date, s.status, s.branch_id, s.iin, s.source,
	coalesce(b.balance_tiyn, 0),
	(SELECT coalesce(json_agg(json_build_object('id', g.id, 'name', g.name, 'course', c.name, 'course_id', c.id, 'type', g.type) ORDER BY g.name), '[]')
		FROM group_students gs JOIN groups g ON g.id = gs.group_id JOIN courses c ON c.id = g.course_id
		WHERE gs.student_id = s.id AND gs.left_at IS NULL),
	(SELECT row_to_json(x) FROM (
		SELECT sb.id, sb.type, sb.name, sb.lessons_total, sb.starts_on, sb.ends_on, sb.status, sb.price_tiyn,
			coalesce((SELECT sum(qty) FROM subscription_usages u WHERE u.subscription_id = sb.id), 0) AS lessons_used,
			(SELECT max(f.ends_on) FROM subscription_freezes f WHERE f.subscription_id = sb.id AND CURRENT_DATE BETWEEN f.starts_on AND f.ends_on) AS frozen_until
		FROM subscriptions sb WHERE sb.student_id = s.id AND sb.status IN ('active', 'frozen', 'used')
		ORDER BY (sb.status = 'active') DESC, sb.starts_on DESC LIMIT 1) x),
	s.custom_fields, s.archived_at, s.created_at
FROM students s LEFT JOIN student_balances b ON b.student_id = s.id`

// Filter строит WHERE по параметрам запроса. Используется списком, экспортом и массовыми операциями.
func Filter(q url.Values, fields []customfield.Field, p *auth.Principal) (string, []any, error) {
	args := []any{}
	add := func(v any) string { args = append(args, v); return "$" + strconv.Itoa(len(args)) }
	conds := []string{}

	if bf := p.BranchFilter(); bf != nil {
		conds = append(conds, "s.branch_id = ANY("+add(bf)+"::uuid[])")
	}
	if s := strings.TrimSpace(q.Get("q")); s != "" {
		digits := strings.Map(func(r rune) rune {
			if unicode.IsDigit(r) {
				return r
			}
			return -1
		}, s)
		c := "(s.full_name ILIKE '%' || " + add(s) + " || '%' OR similarity(s.full_name, $" + strconv.Itoa(len(args)) + ") > 0.35"
		if len(digits) >= 3 {
			c += " OR s.phone LIKE '%' || " + add(digits) + " || '%' OR s.number::text = $" + strconv.Itoa(len(args))
		}
		conds = append(conds, c+")")
	}
	if st := q.Get("status"); st != "" {
		conds = append(conds, "s.status = ANY("+add(strings.Split(st, ","))+"::text[])")
	} else if q.Get("archived") != "1" {
		conds = append(conds, "s.archived_at IS NULL")
	}
	if b := q.Get("branch_id"); b != "" {
		conds = append(conds, "s.branch_id = "+add(b)+"::uuid")
	}
	if g := q.Get("group_id"); g != "" {
		conds = append(conds, "EXISTS (SELECT 1 FROM group_students gs WHERE gs.student_id = s.id AND gs.left_at IS NULL AND gs.group_id = "+add(g)+"::uuid)")
	}
	if c := q.Get("course_id"); c != "" {
		conds = append(conds, "EXISTS (SELECT 1 FROM group_students gs JOIN groups g ON g.id = gs.group_id WHERE gs.student_id = s.id AND gs.left_at IS NULL AND g.course_id = ANY("+add(strings.Split(c, ","))+"::uuid[]))")
	}
	if t := q.Get("teacher_id"); t != "" {
		conds = append(conds, "EXISTS (SELECT 1 FROM group_students gs JOIN groups g ON g.id = gs.group_id WHERE gs.student_id = s.id AND gs.left_at IS NULL AND g.teacher_id = "+add(t)+"::uuid)")
	}
	switch q.Get("balance") {
	case "negative":
		conds = append(conds, "coalesce(b.balance_tiyn, 0) < 0")
	case "positive":
		conds = append(conds, "coalesce(b.balance_tiyn, 0) > 0")
	}
	if q.Get("subscription") == "ending" {
		conds = append(conds, `EXISTS (SELECT 1 FROM subscriptions sb WHERE sb.student_id = s.id AND sb.status = 'active' AND (
			sb.ends_on <= CURRENT_DATE + 7 OR (sb.lessons_total IS NOT NULL AND sb.lessons_total - coalesce((SELECT sum(qty) FROM subscription_usages u WHERE u.subscription_id = sb.id), 0) <= 2)))`)
	}
	if ids := q.Get("ids"); ids != "" {
		conds = append(conds, "s.id = ANY("+add(strings.Split(ids, ","))+"::uuid[])")
	}
	cf, err := customfield.FilterSQL(fields, q, "s.custom_fields", &args, p)
	if err != nil {
		return "", nil, err
	}
	conds = append(conds, cf...)
	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}
	return where, args, nil
}

func scanRows(rows pgx.Rows) ([]Row, error) {
	out := []Row{}
	defer rows.Close()
	for rows.Next() {
		var s Row
		var bal int64
		if err := rows.Scan(&s.ID, &s.Number, &s.FullName, &s.Phone, &s.Email, &s.BirthDate, &s.Status, &s.BranchID, &s.IIN, &s.Source,
			&bal, &s.Groups, &s.Subscription, &s.CustomFields, &s.ArchivedAt, &s.CreatedAt); err != nil {
			return nil, err
		}
		s.Balance = &bal
		out = append(out, s)
	}
	return out, rows.Err()
}

// Present скрывает то, что роль не должна видеть: финансы, ИИН, чувствительные и закрытые поля.
func Present(rows []Row, fields []customfield.Field, p *auth.Principal) {
	for i := range rows {
		r := &rows[i]
		if !p.Can("finance.view") {
			r.Balance = nil
		}
		if r.IIN != nil {
			m := validate.MaskIIN(*r.IIN)
			r.IIN = &m
		}
		r.CustomFields = customfield.Strip(fields, r.CustomFields, p, true)
	}
}

type cursor struct {
	Name string `json:"n"`
	ID   string `json:"i"`
}

func encodeCursor(r Row) string {
	b, _ := json.Marshal(cursor{r.FullName, r.ID})
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(s string) (*cursor, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, httpx.BadRequest("INVALID_CURSOR", "Неверный курсор")
	}
	var c cursor
	if json.Unmarshal(b, &c) != nil {
		return nil, httpx.BadRequest("INVALID_CURSOR", "Неверный курсор")
	}
	return &c, nil
}

// List — keyset-пагинация по (full_name, id).
func List(ctx context.Context, tx pgx.Tx, q url.Values, fields []customfield.Field, p *auth.Principal, limit int) ([]Row, string, int, error) {
	where, args, err := Filter(q, fields, p)
	if err != nil {
		return nil, "", 0, err
	}
	var total int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM students s LEFT JOIN student_balances b ON b.student_id = s.id"+where, args...).Scan(&total); err != nil {
		return nil, "", 0, err
	}
	if c := q.Get("cursor"); c != "" {
		cur, err := decodeCursor(c)
		if err != nil {
			return nil, "", 0, err
		}
		args = append(args, cur.Name, cur.ID)
		kc := fmt.Sprintf("(s.full_name, s.id) > ($%d, $%d::uuid)", len(args)-1, len(args))
		if where == "" {
			where = " WHERE " + kc
		} else {
			where += " AND " + kc
		}
	}
	args = append(args, limit+1)
	rows, err := tx.Query(ctx, listSelect+where+fmt.Sprintf(" ORDER BY s.full_name, s.id LIMIT $%d", len(args)), args...)
	if err != nil {
		return nil, "", 0, err
	}
	list, err := scanRows(rows)
	if err != nil {
		return nil, "", 0, err
	}
	next := ""
	if len(list) > limit {
		list = list[:limit]
		next = encodeCursor(list[len(list)-1])
	}
	return list, next, total, nil
}

func GetRow(ctx context.Context, tx pgx.Tx, id string) (*Row, error) {
	rows, err := tx.Query(ctx, listSelect+" WHERE s.id = $1", id)
	if err != nil {
		return nil, err
	}
	list, err := scanRows(rows)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, httpx.NotFound("STUDENT_NOT_FOUND")
	}
	return &list[0], nil
}
