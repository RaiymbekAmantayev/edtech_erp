package student

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"emp/internal/modules/customfield"
	"emp/internal/modules/finance"
	"emp/internal/platform/auth"
	"emp/internal/platform/database"
	"emp/internal/platform/httpx"
	"emp/internal/platform/validate"

	"github.com/jackc/pgx/v5"
	"github.com/xuri/excelize/v2"
)

// ---------------------------------------------------------------- массовые операции

func (h *Handler) bulk(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		IDs    []string `json:"ids"`
		Action string   `json:"action"` // status, group, responsible, field, archive
		Value  any      `json:"value"`
		Field  string   `json:"field"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if len(req.IDs) == 0 || len(req.IDs) > 1000 {
		return httpx.BadRequest("VALIDATION_FAILED", "Выберите от 1 до 1000 учеников")
	}
	p := auth.FromContext(r.Context())
	ctx := r.Context()
	val, _ := req.Value.(string)
	updated := 0
	err := h.DB.Tx(ctx, func(tx pgx.Tx) error {
		// права на филиалы проверяются через тот же фильтр, что и в списке
		fields, err := customfield.Load(ctx, tx, "student", false)
		if err != nil {
			return err
		}
		where, args, err := Filter(url.Values{"ids": {strings.Join(req.IDs, ",")}, "archived": {"1"}}, fields, p)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, "SELECT s.id::text FROM students s LEFT JOIN student_balances b ON b.student_id=s.id"+where, args...)
		if err != nil {
			return err
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		for _, id := range ids {
			switch req.Action {
			case "status":
				if !slices.Contains(Statuses, val) {
					return httpx.BadRequest("INVALID_STATUS", "Неизвестный статус")
				}
				var old string
				tx.QueryRow(ctx, `SELECT status FROM students WHERE id=$1`, id).Scan(&old)
				if old == val {
					continue
				}
				if _, err := tx.Exec(ctx, `UPDATE students SET status=$2, version=version+1, updated_at=NOW() WHERE id=$1`, id, val); err != nil {
					return err
				}
				database.Audit(ctx, tx, "student", id, "update", map[string]any{"status": old}, map[string]any{"status": val})
				database.Publish(ctx, tx, "student.status_changed", map[string]any{"student_id": id, "from": old, "to": val})
			case "group":
				var exists bool
				tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM group_students WHERE student_id=$1 AND group_id=$2 AND left_at IS NULL)`, id, val).Scan(&exists)
				if exists {
					continue
				}
				// перевод: закрываем текущие членства в группах того же курса
				tx.Exec(ctx, `UPDATE group_students gs SET left_at=CURRENT_DATE, leave_reason='transfer' FROM groups g
					WHERE gs.group_id=g.id AND gs.student_id=$1 AND gs.left_at IS NULL AND g.course_id=(SELECT course_id FROM groups WHERE id=$2)`, id, val)
				if err := joinGroup(ctx, tx, id, val, ""); err != nil {
					return err
				}
			case "responsible":
				if _, err := tx.Exec(ctx, `UPDATE students SET responsible_id=NULLIF($2,'')::uuid, version=version+1 WHERE id=$1`, id, val); err != nil {
					return err
				}
				database.Audit(ctx, tx, "student", id, "update", nil, map[string]any{"responsible_id": val})
			case "field":
				var cur map[string]any
				var status, branch string
				tx.QueryRow(ctx, `SELECT custom_fields, status, branch_id FROM students WHERE id=$1`, id).Scan(&cur, &status, &branch)
				cf, err := customfield.Apply(ctx, tx, fields, cur, map[string]any{req.Field: req.Value}, p,
					customfield.Context{Table: "students", RecordID: id, Status: status, BranchID: branch})
				if err != nil {
					return err
				}
				b, _ := json.Marshal(cf)
				if _, err := tx.Exec(ctx, `UPDATE students SET custom_fields=$2, version=version+1 WHERE id=$1`, id, b); err != nil {
					return err
				}
				database.Audit(ctx, tx, "student", id, "update", map[string]any{"cf." + req.Field: cur[req.Field]}, map[string]any{"cf." + req.Field: req.Value})
			case "archive":
				tx.Exec(ctx, `UPDATE students SET archived_at=NOW(), status='archived', version=version+1 WHERE id=$1 AND archived_at IS NULL`, id)
				tx.Exec(ctx, `UPDATE group_students SET left_at=CURRENT_DATE, leave_reason='archived' WHERE student_id=$1 AND left_at IS NULL`, id)
				database.Audit(ctx, tx, "student", id, "archive", nil, nil)
			default:
				return httpx.BadRequest("INVALID_ACTION", "Неизвестное действие")
			}
			updated++
		}
		return nil
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, map[string]int{"updated": updated})
}

// merge объединяет дубликат с основной записью: переносит платежи, проводки, абонементы, посещаемость, группы, родителей и историю.
func (h *Handler) merge(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		PrimaryID   string `json:"primary_id"`
		DuplicateID string `json:"duplicate_id"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.PrimaryID == "" || req.PrimaryID == req.DuplicateID {
		return httpx.BadRequest("VALIDATION_FAILED", "Укажите две разные записи")
	}
	ctx := r.Context()
	return h.DB.Tx(ctx, func(tx pgx.Tx) error {
		a, b := req.PrimaryID, req.DuplicateID
		for _, q := range []string{
			`UPDATE payments SET student_id=$1 WHERE student_id=$2`,
			`UPDATE subscriptions SET student_id=$1 WHERE student_id=$2`,
			`UPDATE subscription_usages SET student_id=$1 WHERE student_id=$2`,
			`UPDATE student_comments SET student_id=$1 WHERE student_id=$2`,
			`UPDATE attendance SET student_id=$1 WHERE student_id=$2 AND NOT EXISTS (SELECT 1 FROM attendance x WHERE x.lesson_id=attendance.lesson_id AND x.student_id=$1)`,
			`UPDATE group_students SET student_id=$1 WHERE student_id=$2 AND NOT EXISTS (SELECT 1 FROM group_students x WHERE x.group_id=group_students.group_id AND x.student_id=$1 AND x.left_at IS NULL)`,
			`UPDATE group_students SET left_at=CURRENT_DATE, leave_reason='merged' WHERE student_id=$2 AND left_at IS NULL`,
			`INSERT INTO student_parents (tenant_id, student_id, parent_id, relation, is_payer, is_legal_rep)
				SELECT tenant_id, $1, parent_id, relation, is_payer, is_legal_rep FROM student_parents WHERE student_id=$2 ON CONFLICT DO NOTHING`,
			`UPDATE students SET custom_fields = (SELECT custom_fields FROM students WHERE id=$2) || custom_fields, version=version+1 WHERE id=$1`,
			`UPDATE students SET archived_at=NOW(), status='archived', comment=coalesce(comment,'') || ' [объединён с ' || $1::text || ']' WHERE id=$2`,
		} {
			if _, err := tx.Exec(ctx, q, a, b); err != nil {
				return err
			}
		}
		// проводки неизменяемы: переносим остаток корректирующими записями
		var dupBalance int64
		tx.QueryRow(ctx, `SELECT coalesce(sum(amount_tiyn),0) FROM ledger_entries WHERE student_id=$1`, b).Scan(&dupBalance)
		p := auth.FromContext(ctx)
		if dupBalance != 0 {
			if err := finance.Post(ctx, tx, b, "adjustment", -dupBalance, "merge", &a, "Перенос баланса при объединении", p.UserID); err != nil {
				return err
			}
			if err := finance.Post(ctx, tx, a, "adjustment", dupBalance, "merge", &b, "Перенос баланса при объединении", p.UserID); err != nil {
				return err
			}
		}
		return database.Audit(ctx, tx, "student", a, "merge", map[string]any{"duplicate_id": b}, nil)
	})
}

// ---------------------------------------------------------------- экспорт

var stdColumns = []struct{ Key, Label string }{
	{"number", "ID"}, {"full_name", "ФИО"}, {"phone", "Телефон"}, {"email", "Email"}, {"birth_date", "Дата рождения"},
	{"status", "Статус"}, {"groups", "Группы"}, {"balance", "Баланс, ₸"}, {"subscription", "Абонемент"}, {"iin", "ИИН"}, {"source", "Источник"},
}

var statusLabels = map[string]string{"registration": "Регистрация", "active": "Занимается", "frozen": "Заморожен",
	"graduated": "Выпускник", "inactive": "Не занимается", "archived": "Архив"}

func (h *Handler) export(w http.ResponseWriter, r *http.Request) error {
	p := auth.FromContext(r.Context())
	ctx := r.Context()
	format := r.URL.Query().Get("format")
	if format != "csv" {
		format = "xlsx"
	}
	var rows []Row
	var fields []customfield.Field
	err := h.DB.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		if fields, err = customfield.Load(ctx, tx, "student", false); err != nil {
			return err
		}
		q := r.URL.Query()
		q.Del("cursor")
		rows, _, _, err = List(ctx, tx, q, fields, p, 50000)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO export_logs (tenant_id, user_id, entity_type, rows_count, format) VALUES (app_current_tenant(), $1, 'student', $2, $3)`,
			p.UserID, len(rows), format); err != nil {
			return err
		}
		return database.Audit(ctx, tx, "student", "", "export", nil, map[string]any{"rows": len(rows), "format": format, "filters": r.URL.RawQuery})
	})
	if err != nil {
		return err
	}
	Present(rows, fields, p) // ИИН и чувствительные поля — маской, финансы — только с правом
	visible := customfield.Visible(fields, p)
	header := []string{}
	for _, c := range stdColumns {
		if c.Key == "balance" && !p.Can("finance.view") {
			continue
		}
		header = append(header, c.Label)
	}
	for _, f := range visible {
		header = append(header, f.LabelFor("ru"))
	}
	table := [][]string{header}
	for _, s := range rows {
		line := []string{fmt.Sprint(s.Number), s.FullName, validate.FormatPhone(str(s.Phone)), str(s.Email), dateStr(s.BirthDate), statusLabels[s.Status]}
		var groups []struct{ Name string }
		json.Unmarshal(s.Groups, &groups)
		gn := []string{}
		for _, g := range groups {
			gn = append(gn, g.Name)
		}
		line = append(line, strings.Join(gn, ", "))
		if p.Can("finance.view") {
			line = append(line, fmt.Sprint(*s.Balance/100))
		}
		line = append(line, subscriptionText(s.Subscription), str(s.IIN), str(s.Source))
		for i := range visible {
			line = append(line, customfield.Display(&visible[i], s.CustomFields[visible[i].Key], "ru"))
		}
		table = append(table, line)
	}
	name := "students_" + time.Now().Format("2006-01-02")
	if format == "csv" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`.csv"`)
		w.Write([]byte("\xEF\xBB\xBF")) // BOM для Excel
		cw := csv.NewWriter(w)
		cw.Comma = ';'
		cw.WriteAll(table)
		return nil
	}
	f := excelize.NewFile()
	defer f.Close()
	sheet := "Ученики"
	f.SetSheetName("Sheet1", sheet)
	for i, line := range table {
		for j, v := range line {
			cell, _ := excelize.CoordinatesToCellName(j+1, i+1)
			f.SetCellValue(sheet, cell, v)
		}
	}
	style, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	last, _ := excelize.CoordinatesToCellName(len(header), 1)
	f.SetCellStyle(sheet, "A1", last, style)
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`.xlsx"`)
	return f.Write(w)
}

func str(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func dateStr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format("02.01.2006")
}

func subscriptionText(raw json.RawMessage) string {
	var s struct {
		Name         string  `json:"name"`
		LessonsTotal *int    `json:"lessons_total"`
		LessonsUsed  int     `json:"lessons_used"`
		EndsOn       *string `json:"ends_on"`
	}
	if len(raw) == 0 || string(raw) == "null" || json.Unmarshal(raw, &s) != nil {
		return ""
	}
	if s.LessonsTotal != nil {
		return fmt.Sprintf("%s: осталось %d из %d", s.Name, *s.LessonsTotal-s.LessonsUsed, *s.LessonsTotal)
	}
	return s.Name
}

// ---------------------------------------------------------------- импорт

// importFile: multipart file + mapping (JSON {"колонка": "full_name" | "cf.key" | ""}) + branch_id + mode (preview|dry_run|commit) + duplicates (skip|update|create).
func (h *Handler) importFile(w http.ResponseWriter, r *http.Request) error {
	if err := r.ParseMultipartForm(20 << 20); err != nil {
		return httpx.BadRequest("INVALID_FILE", "Не удалось прочитать файл")
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		return httpx.BadRequest("INVALID_FILE", "Файл не передан")
	}
	defer file.Close()
	data, _ := io.ReadAll(file)
	table, err := readTable(hdr.Filename, data)
	if err != nil {
		return httpx.BadRequest("INVALID_FILE", "Поддерживаются файлы XLSX и CSV")
	}
	if len(table) < 2 {
		return httpx.BadRequest("EMPTY_FILE", "В файле нет строк с данными")
	}
	headers := table[0]
	body := table[1:]
	mode := r.FormValue("mode")
	p := auth.FromContext(r.Context())
	ctx := r.Context()

	if mode == "" || mode == "preview" {
		var fields []customfield.Field
		h.DB.Tx(ctx, func(tx pgx.Tx) error { fields, err = customfield.Load(ctx, tx, "student", false); return err })
		preview := body
		if len(preview) > 20 {
			preview = preview[:20]
		}
		return httpx.OK(w, map[string]any{"headers": headers, "rows": preview, "total": len(body), "mapping": suggestMapping(headers, fields),
			"targets": importTargets(customfield.Visible(fields, p))})
	}

	mapping := map[string]string{}
	json.Unmarshal([]byte(r.FormValue("mapping")), &mapping)
	branchID := r.FormValue("branch_id")
	dupMode := r.FormValue("duplicates")
	if branchID == "" || !p.CanBranch(branchID) {
		return httpx.BadRequest("BRANCH_REQUIRED", "Выберите филиал")
	}
	type rowErr struct {
		Row   int    `json:"row"`
		Field string `json:"field"`
		Code  string `json:"code"`
	}
	report := struct {
		Created    int      `json:"created"`
		Updated    int      `json:"updated"`
		Skipped    int      `json:"skipped"`
		Duplicates int      `json:"duplicates"`
		Errors     []rowErr `json:"errors"`
		DryRun     bool     `json:"dry_run"`
	}{Errors: []rowErr{}, DryRun: mode != "commit"}

	errDryRun := fmt.Errorf("dry run")
	err = h.DB.Tx(ctx, func(tx pgx.Tx) error {
		fields, err := customfield.Load(ctx, tx, "student", false)
		if err != nil {
			return err
		}
		byKey := customfield.ByKey(fields)
		for i, line := range body {
			rowNum := i + 2
			std := map[string]string{}
			cfPatch := map[string]any{}
			for ci, col := range headers {
				target := mapping[col]
				if target == "" || ci >= len(line) {
					continue
				}
				cell := strings.TrimSpace(line[ci])
				if k, ok := strings.CutPrefix(target, "cf."); ok {
					if f := byKey[k]; f != nil && cell != "" {
						cfPatch[k] = customfield.ParseImportValue(f, cell)
					}
				} else {
					std[target] = cell
				}
			}
			if strings.TrimSpace(std["full_name"]) == "" {
				if strings.Join(line, "") == "" {
					continue
				}
				report.Errors = append(report.Errors, rowErr{rowNum, "full_name", "REQUIRED"})
				continue
			}
			in := input{FullName: ptr(std["full_name"]), Phone: ptr(std["phone"]), Email: ptr(std["email"]), IIN: ptr(std["iin"]), Source: ptr(std["source"])}
			if d := std["birth_date"]; d != "" {
				in.BirthDate = ptr(parseDate(d))
			}
			status := "active"
			if s := std["status"]; s != "" {
				status = parseStatus(s)
			}
			v := &httpx.Validation{}
			in.normalize(v)
			if len(v.Fields) > 0 {
				for _, fe := range v.Fields {
					report.Errors = append(report.Errors, rowErr{rowNum, fe.Field, fe.Code})
				}
				continue
			}
			// поиск дубликата по телефону
			var dupID string
			if *in.Phone != "" {
				tx.QueryRow(ctx, `SELECT id FROM students WHERE phone=$1 AND archived_at IS NULL LIMIT 1`, *in.Phone).Scan(&dupID)
			}
			if dupID != "" {
				report.Duplicates++
				if dupMode == "skip" || dupMode == "" {
					report.Skipped++
					continue
				}
			}
			// каждая строка — в своей точке сохранения, ошибка строки не откатывает весь импорт
			sp, err := tx.Begin(ctx)
			if err != nil {
				return err
			}
			rowErrFn := func(e error) {
				sp.Rollback(ctx)
				if he, ok := e.(*httpx.Error); ok && len(he.Fields) > 0 {
					for _, fe := range he.Fields {
						report.Errors = append(report.Errors, rowErr{rowNum, fe.Field, fe.Code})
					}
				} else {
					report.Errors = append(report.Errors, rowErr{rowNum, "", "ROW_FAILED"})
				}
			}
			if dupID != "" && dupMode == "update" {
				var cur map[string]any
				sp.QueryRow(ctx, `SELECT custom_fields FROM students WHERE id=$1`, dupID).Scan(&cur)
				cf, err := customfield.Apply(ctx, sp, fields, cur, toAnyMap(cfPatch), p, customfield.Context{Table: "students", RecordID: dupID, Status: status, BranchID: branchID})
				if err != nil {
					rowErrFn(err)
					continue
				}
				b, _ := json.Marshal(cf)
				if _, err := sp.Exec(ctx, `UPDATE students SET full_name=$2, email=COALESCE($3, email), custom_fields=$4, version=version+1 WHERE id=$1`,
					dupID, *in.FullName, nz(in.Email), b); err != nil {
					rowErrFn(err)
					continue
				}
				database.Audit(ctx, sp, "student", dupID, "import_update", nil, map[string]any{"row": rowNum})
				sp.Commit(ctx)
				report.Updated++
				continue
			}
			cf, err := customfield.Apply(ctx, sp, fields, nil, toAnyMap(cfPatch), p, customfield.Context{Table: "students", Status: status, BranchID: branchID, Creating: true})
			if err != nil {
				rowErrFn(err)
				continue
			}
			b, _ := json.Marshal(cf)
			var id string
			if err := sp.QueryRow(ctx, `INSERT INTO students (tenant_id, branch_id, full_name, phone, email, birth_date, iin, source, status, custom_fields, started_at)
				VALUES (app_current_tenant(), $1, $2, $3, $4, NULLIF($5,'')::date, $6, $7, $8, $9, CASE WHEN $8='active' THEN CURRENT_DATE END) RETURNING id`,
				branchID, *in.FullName, nz(in.Phone), nz(in.Email), deref(in.BirthDate), nz(in.IIN), nz(in.Source), status, b).Scan(&id); err != nil {
				rowErrFn(err)
				continue
			}
			sp.Exec(ctx, `INSERT INTO student_balances (tenant_id, student_id) VALUES (app_current_tenant(), $1)`, id)
			if pn := std["parent_name"]; pn != "" {
				if err := attachParent(ctx, sp, id, parentInput{FullName: pn, Phone: std["parent_phone"], Relation: "parent", IsPayer: true, IsLegalRep: true}); err != nil {
					rowErrFn(err)
					continue
				}
			}
			if bal := std["balance"]; bal != "" {
				if amount, ok := parseMoney(bal); ok && amount != 0 {
					finance.Post(ctx, sp, id, "adjustment", amount, "import", nil, "Начальный баланс при импорте", p.UserID)
				}
			}
			database.Audit(ctx, sp, "student", id, "import", nil, map[string]any{"row": rowNum, "file": hdr.Filename})
			if err := sp.Commit(ctx); err != nil {
				return err
			}
			report.Created++
		}
		if mode != "commit" {
			return errDryRun // пробный прогон: откатываем всё и отдаём отчёт расхождений
		}
		return nil
	})
	if err != nil && err != errDryRun {
		return err
	}
	return httpx.OK(w, report)
}

func toAnyMap(m map[string]any) map[string]any { return m }

func ptr(s string) *string { return &s }

func readTable(name string, data []byte) ([][]string, error) {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".xlsx":
		f, err := excelize.OpenReader(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return f.GetRows(f.GetSheetName(0))
	case ".csv", ".txt":
		data = bytes.TrimPrefix(data, []byte("\xEF\xBB\xBF"))
		sep := ','
		firstLine := string(data[:bytes.IndexByte(append(data, '\n'), '\n')])
		if strings.Count(firstLine, ";") > strings.Count(firstLine, ",") {
			sep = ';'
		}
		cr := csv.NewReader(bytes.NewReader(data))
		cr.Comma = sep
		cr.FieldsPerRecord = -1
		cr.LazyQuotes = true
		return cr.ReadAll()
	}
	return nil, fmt.Errorf("unsupported")
}

type target struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

func importTargets(fields []customfield.Field) []target {
	t := []target{{"full_name", "ФИО ученика"}, {"phone", "Телефон"}, {"email", "Email"}, {"birth_date", "Дата рождения"},
		{"iin", "ИИН"}, {"status", "Статус"}, {"source", "Источник"}, {"parent_name", "ФИО родителя"}, {"parent_phone", "Телефон родителя"},
		{"balance", "Начальный баланс, ₸"}}
	for _, f := range fields {
		t = append(t, target{"cf." + f.Key, f.LabelFor("ru") + " (доп. поле)"})
	}
	return t
}

var synonyms = map[string][]string{
	"full_name": {"фио", "ученик", "имя", "name", "full name", "аты-жөні", "студент"},
	"phone":     {"телефон", "phone", "тел", "мобильный", "номер"},
	"email":     {"email", "e-mail", "почта"},
	"birth_date": {"дата рождения", "др", "birth", "туған күні"},
	"iin":          {"иин", "iin"},
	"status":       {"статус", "status"},
	"source":       {"источник", "source"},
	"parent_name":  {"родитель", "фио родителя", "мама", "parent"},
	"parent_phone": {"телефон родителя", "тел. родителя", "parent phone"},
	"balance":      {"баланс", "долг", "balance"},
}

func suggestMapping(headers []string, fields []customfield.Field) map[string]string {
	m := map[string]string{}
	used := map[string]bool{}
	for _, h := range headers {
		l := strings.ToLower(strings.TrimSpace(h))
		best := ""
		for _, f := range fields {
			if strings.EqualFold(f.LabelFor("ru"), l) || f.Key == l {
				best = "cf." + f.Key
			}
		}
		if best == "" {
			for key, syn := range synonyms {
				for _, s := range syn {
					if l == s {
						best = key
					}
				}
			}
		}
		if best == "" {
			for _, key := range []string{"parent_phone", "parent_name", "full_name", "phone", "email", "birth_date", "iin", "status", "source", "balance"} {
				for _, s := range synonyms[key] {
					if strings.Contains(l, s) && !used[key] {
						best = key
						break
					}
				}
				if best != "" {
					break
				}
			}
		}
		if best != "" && !used[best] {
			m[h] = best
			used[best] = true
		} else {
			m[h] = ""
		}
	}
	return m
}

func parseDate(s string) string {
	for _, layout := range []string{"02.01.2006", "2006-01-02", "02/01/2006", "2.1.2006", "01-02-06"} {
		if t, err := time.Parse(layout, strings.TrimSpace(s)); err == nil {
			return t.Format("2006-01-02")
		}
	}
	return s
}

func parseStatus(s string) string {
	l := strings.ToLower(strings.TrimSpace(s))
	for k, v := range statusLabels {
		if strings.ToLower(v) == l || k == l {
			return k
		}
	}
	return "active"
}

// parseMoney: «-18 000», «18000,50» → тиын.
func parseMoney(s string) (int64, bool) {
	s = strings.NewReplacer(" ", "", " ", "", "₸", "", "тг", "").Replace(s)
	s = strings.ReplaceAll(s, ",", ".")
	neg := strings.HasPrefix(s, "-") || strings.HasPrefix(s, "−")
	s = strings.TrimLeft(s, "-−")
	whole, frac, _ := strings.Cut(s, ".")
	var w, f int64
	if _, err := fmt.Sscan(whole, &w); err != nil {
		return 0, false
	}
	if frac != "" {
		if len(frac) == 1 {
			frac += "0"
		}
		fmt.Sscan(frac[:2], &f)
	}
	v := w*100 + f
	if neg {
		v = -v
	}
	return v, true
}
