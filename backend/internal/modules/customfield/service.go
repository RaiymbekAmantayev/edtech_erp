// Пакет customfield — пользовательские поля (ТЗ раздел 9).
// Определения хранятся в custom_field_definitions, значения — в колонке custom_fields JSONB у сущности.
package customfield

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"emp/internal/platform/auth"
	"emp/internal/platform/httpx"
	"emp/internal/platform/validate"

	"github.com/jackc/pgx/v5"
)

// Сущность → таблица. Новая сущность подключается регистрацией здесь, без изменения схемы под каждое поле.
var EntityTables = map[string]string{
	"student": "students", "parent": "parents", "lead": "leads", "group": "groups",
	"employee": "employees", "payment": "payments", "course": "courses",
}

// Стандартные поля сущностей — ключ пользовательского поля не может с ними совпадать.
var reservedKeys = []string{"id", "full_name", "name", "phone", "email", "status", "branch_id", "birth_date", "iin",
	"address", "source", "comment", "created_at", "updated_at", "tenant_id", "custom_fields", "version", "number"}

var FieldTypes = []string{"string", "text", "integer", "decimal", "money", "date", "datetime", "boolean",
	"select", "multiselect", "phone", "email", "url", "file", "employee", "relation"}

type Settings struct {
	Required       string   `json:"required,omitempty"` // "" | always | on_status
	RequiredStatus string   `json:"required_status,omitempty"`
	Min            *float64 `json:"min,omitempty"`
	Max            *float64 `json:"max,omitempty"`
	Precision      *int     `json:"precision,omitempty"`
	Mask           string   `json:"mask,omitempty"`
	Placeholder    string   `json:"placeholder,omitempty"`
	Hint           string   `json:"hint,omitempty"`
	Default        any      `json:"default,omitempty"`
	BranchIDs      []string `json:"branch_ids,omitempty"`
	CourseIDs      []string `json:"course_ids,omitempty"`
	RolesView      []string `json:"roles_view,omitempty"`
	RolesEdit      []string `json:"roles_edit,omitempty"`
	ShowInTable    bool     `json:"show_in_table,omitempty"`
	ShowInCreate   bool     `json:"show_in_create,omitempty"`
	ShowInTeacher  bool     `json:"show_in_teacher,omitempty"`
	ShowInParent   bool     `json:"show_in_parent,omitempty"`
	ShowInWebForm  bool     `json:"show_in_web_form,omitempty"`
	RelationEntity string   `json:"relation_entity,omitempty"`
}

type Option struct {
	ID         string            `json:"id"`
	Label      map[string]string `json:"label"`
	Color      *string           `json:"color"`
	SortOrder  int               `json:"sort_order"`
	ArchivedAt *time.Time        `json:"archived_at"`
}

type Field struct {
	ID          string            `json:"id"`
	EntityType  string            `json:"entity_type"`
	Key         string            `json:"key"`
	Type        string            `json:"field_type"`
	Label       map[string]string `json:"label"`
	BlockID     *string           `json:"block_id"`
	SortOrder   int               `json:"sort_order"`
	Settings    Settings          `json:"settings"`
	IsSensitive bool              `json:"is_sensitive"`
	IsUnique    bool              `json:"is_unique"`
	ArchivedAt  *time.Time        `json:"archived_at"`
	Options     []Option          `json:"options"`
}

func (f *Field) LabelFor(locale string) string {
	if v := f.Label[locale]; v != "" {
		return v
	}
	return f.Label["ru"]
}

func (f *Field) option(id string) *Option {
	for i := range f.Options {
		if f.Options[i].ID == id {
			return &f.Options[i]
		}
	}
	return nil
}

// Load возвращает определения полей сущности (включая архивные, если includeArchived).
func Load(ctx context.Context, tx pgx.Tx, entity string, includeArchived bool) ([]Field, error) {
	rows, err := tx.Query(ctx, `SELECT id, entity_type, key, field_type, label, block_id, sort_order, settings, is_sensitive, is_unique, archived_at
		FROM custom_field_definitions WHERE entity_type=$1 AND ($2 OR archived_at IS NULL) ORDER BY sort_order, created_at`, entity, includeArchived)
	if err != nil {
		return nil, err
	}
	fields := []Field{}
	idx := map[string]int{}
	for rows.Next() {
		var f Field
		var settings []byte
		if err := rows.Scan(&f.ID, &f.EntityType, &f.Key, &f.Type, &f.Label, &f.BlockID, &f.SortOrder, &settings, &f.IsSensitive, &f.IsUnique, &f.ArchivedAt); err != nil {
			return nil, err
		}
		json.Unmarshal(settings, &f.Settings)
		f.Options = []Option{}
		idx[f.ID] = len(fields)
		fields = append(fields, f)
	}
	rows.Close()
	if len(fields) == 0 {
		return fields, nil
	}
	orows, err := tx.Query(ctx, `SELECT o.id, o.field_id, o.label, o.color, o.sort_order, o.archived_at FROM custom_field_options o
		JOIN custom_field_definitions d ON d.id=o.field_id WHERE d.entity_type=$1 ORDER BY o.sort_order`, entity)
	if err != nil {
		return nil, err
	}
	defer orows.Close()
	for orows.Next() {
		var o Option
		var fid string
		if err := orows.Scan(&o.ID, &fid, &o.Label, &o.Color, &o.SortOrder, &o.ArchivedAt); err != nil {
			return nil, err
		}
		if i, ok := idx[fid]; ok {
			fields[i].Options = append(fields[i].Options, o)
		}
	}
	return fields, nil
}

func ByKey(fields []Field) map[string]*Field {
	m := make(map[string]*Field, len(fields))
	for i := range fields {
		m[fields[i].Key] = &fields[i]
	}
	return m
}

// ---------------------------------------------------------------- доступ по ролям

func roleMatch(p *auth.Principal, roles []string) bool {
	if len(roles) == 0 || p.HasRole("owner") {
		return true
	}
	for _, r := range roles {
		if p.HasRole(r) {
			return true
		}
	}
	return false
}

func CanView(f *Field, p *auth.Principal) bool { return roleMatch(p, f.Settings.RolesView) }
func CanEdit(f *Field, p *auth.Principal) bool {
	return CanView(f, p) && roleMatch(p, f.Settings.RolesEdit)
}

// Visible возвращает определения, видимые пользователю (для UI).
func Visible(fields []Field, p *auth.Principal) []Field {
	out := []Field{}
	for i := range fields {
		if CanView(&fields[i], p) {
			out = append(out, fields[i])
		}
	}
	return out
}

const Masked = "••••••"

// Strip удаляет из значений поля, которые роль не видит, и маскирует чувствительные (если mask).
func Strip(fields []Field, values map[string]any, p *auth.Principal, mask bool) map[string]any {
	out := map[string]any{}
	byKey := ByKey(fields)
	for k, v := range values {
		f, ok := byKey[k]
		if !ok || !CanView(f, p) {
			continue
		}
		if mask && f.IsSensitive && v != nil && v != "" {
			out[k] = Masked
			continue
		}
		out[k] = v
	}
	return out
}

// ---------------------------------------------------------------- валидация

type Context struct {
	Table     string // таблица сущности для проверки уникальности
	RecordID  string // id изменяемой записи ("" при создании)
	Status    string // статус записи после изменения (для «обязательно при статусе»)
	CourseIDs []string
	BranchID  string
	Creating  bool
}

func inScope(f *Field, c Context) bool {
	if len(f.Settings.BranchIDs) > 0 && c.BranchID != "" && !slices.Contains(f.Settings.BranchIDs, c.BranchID) {
		return false
	}
	if len(f.Settings.CourseIDs) > 0 {
		for _, id := range c.CourseIDs {
			if slices.Contains(f.Settings.CourseIDs, id) {
				return true
			}
		}
		return false
	}
	return true
}

// Apply валидирует patch по определениям и возвращает итоговое значение custom_fields.
// Значения валидируются на сервере; клиентская валидация — только для удобства.
func Apply(ctx context.Context, tx pgx.Tx, fields []Field, existing, patch map[string]any, p *auth.Principal, c Context) (map[string]any, error) {
	byKey := ByKey(fields)
	merged := map[string]any{}
	for k, v := range existing {
		merged[k] = v
	}
	if c.Creating {
		for i := range fields {
			f := &fields[i]
			if _, ok := patch[f.Key]; !ok && f.Settings.Default != nil {
				merged[f.Key] = f.Settings.Default
			}
		}
	}
	v := &httpx.Validation{}
	for key, raw := range patch {
		f, ok := byKey[key]
		if !ok {
			return nil, &httpx.Error{Status: 422, Code: "CUSTOM_FIELD_UNKNOWN", Message: "Неизвестное поле: " + key,
				Fields: []httpx.FieldError{{Field: "custom_fields." + key, Code: "CUSTOM_FIELD_UNKNOWN"}}}
		}
		if !CanEdit(f, p) {
			if raw == Masked || equalJSON(merged[key], raw) {
				continue
			}
			return nil, &httpx.Error{Status: 403, Code: "CUSTOM_FIELD_READ_ONLY", Message: "Нет прав на изменение поля «" + f.LabelFor("ru") + "»",
				Fields: []httpx.FieldError{{Field: "custom_fields." + key, Code: "CUSTOM_FIELD_READ_ONLY"}}}
		}
		if f.IsSensitive && raw == Masked {
			continue // маска пришла обратно с клиента — значение не меняем
		}
		val, code := normalize(f, raw)
		if code != "" {
			v.Add("custom_fields."+key, code)
			continue
		}
		if val == nil {
			delete(merged, key)
		} else {
			merged[key] = val
		}
	}
	for i := range fields {
		f := &fields[i]
		if !inScope(f, c) {
			continue
		}
		required := f.Settings.Required == "always" || (f.Settings.Required == "on_status" && f.Settings.RequiredStatus == c.Status)
		if required && isEmpty(merged[f.Key]) {
			v.Add("custom_fields."+f.Key, "REQUIRED")
		}
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	// Уникальность: проверка в транзакции под advisory-блокировкой по (tenant, поле).
	for key := range patch {
		f := byKey[key]
		val, ok := merged[key]
		if !f.IsUnique || !ok || isEmpty(val) {
			continue
		}
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext(app_current_tenant()::text || $1))`, c.Table+"."+key); err != nil {
			return nil, err
		}
		var dup bool
		q := fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM %s WHERE custom_fields->>$1 = $2 AND ($3 = '' OR id::text <> $3))`, pgx.Identifier{c.Table}.Sanitize())
		if err := tx.QueryRow(ctx, q, key, fmt.Sprint(val), c.RecordID).Scan(&dup); err != nil {
			return nil, err
		}
		if dup {
			return nil, &httpx.Error{Status: 422, Code: "VALIDATION_FAILED", Message: "Значение «" + f.LabelFor("ru") + "» уже используется",
				Fields: []httpx.FieldError{{Field: "custom_fields." + key, Code: "NOT_UNIQUE"}}}
		}
	}
	return merged, nil
}

func equalJSON(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

func isEmpty(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(t) == ""
	case []any:
		return len(t) == 0
	case []string:
		return len(t) == 0
	}
	return false
}

func toFloat(raw any) (float64, bool) {
	switch t := raw.(type) {
	case float64:
		return t, true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case json.Number:
		f, err := t.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(t), " ", ""), ",", "."), 64)
		return f, err == nil
	}
	return 0, false
}

// normalize приводит значение к формату хранения. Возвращает код ошибки валидации.
func normalize(f *Field, raw any) (any, string) {
	if isEmpty(raw) {
		return nil, ""
	}
	s := f.Settings
	checkRange := func(x float64) string {
		if s.Min != nil && x < *s.Min {
			return "TOO_SMALL"
		}
		if s.Max != nil && x > *s.Max {
			return "TOO_LARGE"
		}
		return ""
	}
	str := func() (string, bool) { v, ok := raw.(string); return strings.TrimSpace(v), ok }
	switch f.Type {
	case "string":
		v, ok := str()
		if !ok {
			v = fmt.Sprint(raw)
		}
		if len([]rune(v)) > 255 {
			return nil, "TOO_LONG"
		}
		if s.Mask != "" {
			if re, err := regexp.Compile("^(?:" + s.Mask + ")$"); err == nil && !re.MatchString(v) {
				return nil, "INVALID_FORMAT"
			}
		}
		return v, ""
	case "text":
		v, ok := str()
		if !ok {
			return nil, "INVALID_TYPE"
		}
		if len([]rune(v)) > 10000 {
			return nil, "TOO_LONG"
		}
		return v, ""
	case "integer":
		x, ok := toFloat(raw)
		if !ok || x != math.Trunc(x) {
			return nil, "INVALID_NUMBER"
		}
		if c := checkRange(x); c != "" {
			return nil, c
		}
		return int64(x), ""
	case "decimal":
		x, ok := toFloat(raw)
		if !ok {
			return nil, "INVALID_NUMBER"
		}
		if c := checkRange(x); c != "" {
			return nil, c
		}
		if s.Precision != nil {
			p := math.Pow(10, float64(*s.Precision))
			x = math.Round(x*p) / p
		}
		return x, ""
	case "money":
		// деньги передаются в тиын (целое число), без float
		x, ok := toFloat(raw)
		if !ok || x != math.Trunc(x) {
			return nil, "INVALID_MONEY"
		}
		return int64(x), ""
	case "date":
		v, _ := str()
		if _, err := time.Parse("2006-01-02", v); err != nil {
			return nil, "INVALID_DATE"
		}
		return v, ""
	case "datetime":
		v, _ := str()
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return nil, "INVALID_DATE"
		}
		return t.UTC().Format(time.RFC3339), ""
	case "boolean":
		b, ok := raw.(bool)
		if !ok {
			return nil, "INVALID_TYPE"
		}
		return b, ""
	case "select":
		v, _ := str()
		o := f.option(v)
		if o == nil || o.ArchivedAt != nil {
			return nil, "INVALID_OPTION"
		}
		return v, ""
	case "multiselect":
		arr, ok := raw.([]any)
		if !ok {
			return nil, "INVALID_TYPE"
		}
		out := []string{}
		for _, a := range arr {
			id, _ := a.(string)
			if o := f.option(id); o == nil {
				return nil, "INVALID_OPTION"
			}
			if !slices.Contains(out, id) {
				out = append(out, id)
			}
		}
		return out, ""
	case "phone":
		v, _ := str()
		ph, ok := validate.Phone(v)
		if !ok {
			return nil, "INVALID_FORMAT"
		}
		return ph, ""
	case "email":
		v, _ := str()
		if !validate.Email(v) {
			return nil, "INVALID_FORMAT"
		}
		return v, ""
	case "url":
		v, _ := str()
		u, err := url.Parse(v)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, "INVALID_URL"
		}
		return v, ""
	case "file", "employee", "relation":
		v, ok := str()
		if !ok {
			return nil, "INVALID_TYPE"
		}
		return v, ""
	}
	return nil, "INVALID_TYPE"
}

// Display возвращает человекочитаемое значение (экспорт, шаблоны).
func Display(f *Field, v any, locale string) string {
	if v == nil {
		return ""
	}
	switch f.Type {
	case "select":
		if o := f.option(fmt.Sprint(v)); o != nil {
			return labelOf(o.Label, locale)
		}
	case "multiselect":
		parts := []string{}
		if arr, ok := v.([]any); ok {
			for _, a := range arr {
				if o := f.option(fmt.Sprint(a)); o != nil {
					parts = append(parts, labelOf(o.Label, locale))
				}
			}
		}
		return strings.Join(parts, ", ")
	case "boolean":
		if b, _ := v.(bool); b {
			return "Да"
		}
		return "Нет"
	case "money":
		x, _ := toFloat(v)
		return strconv.FormatInt(int64(x)/100, 10)
	}
	if fl, ok := v.(float64); ok {
		return strconv.FormatFloat(fl, 'f', -1, 64)
	}
	return fmt.Sprint(v)
}

func labelOf(m map[string]string, locale string) string {
	if v := m[locale]; v != "" {
		return v
	}
	return m["ru"]
}

// ParseImportValue переводит значение из файла импорта во внутреннее представление (варианты — по названию).
func ParseImportValue(f *Field, s string) any {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	switch f.Type {
	case "select":
		for _, o := range f.Options {
			if strings.EqualFold(labelOf(o.Label, "ru"), s) || o.ID == s {
				return o.ID
			}
		}
		return s // будет отклонено валидацией как INVALID_OPTION
	case "multiselect":
		out := []any{}
		for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' }) {
			part = strings.TrimSpace(part)
			found := part
			for _, o := range f.Options {
				if strings.EqualFold(labelOf(o.Label, "ru"), part) {
					found = o.ID
				}
			}
			out = append(out, found)
		}
		return out
	case "boolean":
		l := strings.ToLower(s)
		return l == "да" || l == "yes" || l == "true" || l == "1" || l == "иә"
	case "integer", "decimal", "money":
		return s
	}
	return s
}

// ---------------------------------------------------------------- фильтры

// FilterSQL строит условия WHERE для параметров вида cf.key=…, cf.key[gte]=…, cf.key[contains]=…, cf.key[empty]=1.
// col — выражение колонки custom_fields (например "s.custom_fields"). Поля, которые роль не видит, фильтровать нельзя.
func FilterSQL(fields []Field, q url.Values, col string, args *[]any, p *auth.Principal) ([]string, error) {
	byKey := ByKey(fields)
	conds := []string{}
	add := func(v any) string {
		*args = append(*args, v)
		return "$" + strconv.Itoa(len(*args))
	}
	for param, vals := range q {
		if !strings.HasPrefix(param, "cf.") || len(vals) == 0 || vals[0] == "" {
			continue
		}
		key, op := strings.TrimPrefix(param, "cf."), "eq"
		if i := strings.Index(key, "["); i > 0 && strings.HasSuffix(key, "]") {
			key, op = key[:i], key[i+1:len(key)-1]
		}
		f, ok := byKey[key]
		if !ok || !CanView(f, p) {
			return nil, httpx.BadRequest("CUSTOM_FIELD_UNKNOWN", "Неизвестное поле фильтра: "+key)
		}
		val := vals[0]
		k := add(key)
		text := fmt.Sprintf("(%s->>%s)", col, k)
		switch op {
		case "empty":
			if val == "1" || val == "true" {
				conds = append(conds, fmt.Sprintf("(%s->%s IS NULL OR %s = '')", col, k, text))
			} else {
				conds = append(conds, fmt.Sprintf("(%s->%s IS NOT NULL AND %s <> '')", col, k, text))
			}
		case "gte", "lte", "gt", "lt":
			sym := map[string]string{"gte": ">=", "lte": "<=", "gt": ">", "lt": "<"}[op]
			switch f.Type {
			case "integer", "decimal", "money":
				x, ok := toFloat(val)
				if !ok {
					return nil, httpx.BadRequest("INVALID_FILTER", "Неверное число в фильтре")
				}
				conds = append(conds, fmt.Sprintf("(CASE WHEN jsonb_typeof(%s->%s)='number' THEN %s::numeric END) %s %s", col, k, text, sym, add(x)))
			case "date", "datetime":
				conds = append(conds, fmt.Sprintf("%s %s %s", text, sym, add(val)))
			default:
				return nil, httpx.BadRequest("INVALID_FILTER", "Оператор не поддерживается для поля "+key)
			}
		case "contains":
			conds = append(conds, fmt.Sprintf("%s ILIKE '%%' || %s || '%%'", text, add(val)))
		case "eq", "in", "any", "all":
			switch f.Type {
			case "select":
				ids := resolveOptions(f, strings.Split(val, ","))
				conds = append(conds, fmt.Sprintf("%s = ANY(%s::text[])", text, add(ids)))
			case "multiselect":
				ids := resolveOptions(f, strings.Split(val, ","))
				if op == "all" {
					conds = append(conds, fmt.Sprintf("%s->%s ?& %s::text[]", col, k, add(ids)))
				} else {
					conds = append(conds, fmt.Sprintf("%s->%s ?| %s::text[]", col, k, add(ids)))
				}
			case "boolean":
				b, _ := strconv.ParseBool(val)
				conds = append(conds, fmt.Sprintf("%s @> jsonb_build_object(%s::text, %s::boolean)", col, k, add(b)))
			case "integer", "decimal", "money":
				x, ok := toFloat(val)
				if !ok {
					return nil, httpx.BadRequest("INVALID_FILTER", "Неверное число в фильтре")
				}
				conds = append(conds, fmt.Sprintf("(CASE WHEN jsonb_typeof(%s->%s)='number' THEN %s::numeric END) = %s", col, k, text, add(x)))
			case "string", "text", "phone", "email":
				conds = append(conds, fmt.Sprintf("lower(%s) = lower(%s)", text, add(val)))
			default:
				conds = append(conds, fmt.Sprintf("%s = %s", text, add(val)))
			}
		default:
			return nil, httpx.BadRequest("INVALID_FILTER", "Неизвестный оператор "+op)
		}
	}
	return conds, nil
}

// resolveOptions принимает ID варианта или его название (для удобства «Видов» и ссылок).
func resolveOptions(f *Field, vals []string) []string {
	out := []string{}
	for _, v := range vals {
		v = strings.TrimSpace(v)
		id := v
		for _, o := range f.Options {
			if o.ID == v || strings.EqualFold(labelOf(o.Label, "ru"), v) {
				id = o.ID
			}
		}
		out = append(out, id)
	}
	return out
}

// ---------------------------------------------------------------- ключи

var translit = map[rune]string{'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "e", 'ж': "zh", 'з': "z", 'и': "i", 'й': "y",
	'к': "k", 'л': "l", 'м': "m", 'н': "n", 'о': "o", 'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u", 'ф': "f", 'х': "h", 'ц': "ts",
	'ч': "ch", 'ш': "sh", 'щ': "sch", 'ъ': "", 'ы': "y", 'ь': "", 'э': "e", 'ю': "yu", 'я': "ya",
	'ә': "a", 'ғ': "g", 'қ': "q", 'ң': "n", 'ө': "o", 'ұ': "u", 'ү': "u", 'һ': "h", 'і': "i"}

// GenerateKey: «Целевой балл ЕНТ» → celevoy_ball_ent.
func GenerateKey(label string) string {
	var b strings.Builder
	prevUnderscore := true
	for _, r := range strings.ToLower(label) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			prevUnderscore = false
		case translit[r] != "" || r == 'ъ' || r == 'ь':
			b.WriteString(translit[r])
			prevUnderscore = false
		default:
			if !prevUnderscore {
				b.WriteRune('_')
				prevUnderscore = true
			}
		}
	}
	k := strings.Trim(b.String(), "_")
	if len(k) > 50 {
		k = k[:50]
	}
	if k == "" || (k[0] >= '0' && k[0] <= '9') {
		k = "field_" + k
	}
	return k
}

var keyRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

func ValidKey(k string) bool { return keyRe.MatchString(k) && !slices.Contains(reservedKeys, k) }
