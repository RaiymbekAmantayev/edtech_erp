package organization

import (
	"context"
	"encoding/json"

	"emp/internal/platform/auth"

	"github.com/jackc/pgx/v5"
)

// Bootstrap создаёт системные роли, правила списания, шаблон полей отрасли и общие «Виды».
// Вызывается внутри транзакции с установленным app.tenant_id.
func Bootstrap(ctx context.Context, tx pgx.Tx, tenantID, template string) (ownerRoleID string, err error) {
	for _, r := range auth.SystemRoles {
		var id string
		if err = tx.QueryRow(ctx, `INSERT INTO roles (tenant_id, code, name, is_system, all_branches, permissions)
			VALUES ($1,$2,$3,TRUE,$4,$5) RETURNING id`, tenantID, r.Code, r.Name, r.AllBranches, r.Permissions).Scan(&id); err != nil {
			return
		}
		if r.Code == "owner" {
			ownerRoleID = id
		}
	}
	// ТЗ 12.6: какой статус посещаемости списывает занятие и учитывается в зарплате.
	rules := []struct {
		s      string
		charge bool
		pay    bool
	}{{"present", true, true}, {"late", true, true}, {"absent", true, true}, {"excused", false, false}, {"trial", false, true}}
	for _, r := range rules {
		if _, err = tx.Exec(ctx, `INSERT INTO attendance_charge_rules (tenant_id, status, charge, pay_teacher) VALUES ($1,$2,$3,$4)`,
			tenantID, r.s, r.charge, r.pay); err != nil {
			return
		}
	}
	err = applyFieldTemplate(ctx, tx, tenantID, template)
	if err != nil {
		return
	}
	views := []struct{ name, cfg string }{
		{"Должники", `{"filters":{"balance":"negative"}}`},
		{"Абонемент заканчивается", `{"filters":{"subscription":"ending"}}`},
	}
	if template == "ent" {
		views = append([]struct{ name, cfg string }{{"ЕНТ · 11 класс", `{"filters":{"cf.grade":"11"}}`}}, views...)
	}
	for i, v := range views {
		if _, err = tx.Exec(ctx, `INSERT INTO saved_views (tenant_id, entity_type, name, config, sort_order) VALUES ($1,'student',$2,$3,$4)`,
			tenantID, v.name, v.cfg, i); err != nil {
			return
		}
	}
	return
}

// DefaultSettings — стартовые настройки организации.
func DefaultSettings(size string) map[string]any {
	return map[string]any{
		"lead_sources":       []string{"Instagram", "2GIS", "Сайт", "Рекомендация", "Звонок", "WhatsApp"},
		"lost_reasons":       []string{"Дорого", "Не подошло расписание", "Выбрали другой центр", "Не отвечает", "Передумали"},
		"lesson_on_holiday":  "needs_decision", // skip | needs_decision
		"edit_closed_hours":  24,
		"excused_limit":      0, // уважительных пропусков в месяц без списания (0 — без лимита)
		"quiet_hours":        map[string]string{"from": "22:00", "to": "08:00"},
		"student_statuses":   []string{"registration", "active", "frozen", "graduated", "inactive", "archived"},
		"size":               size,
		"credit_lessons":     2,
	}
}

type tplField struct {
	key, typ, label string
	settings        map[string]any
	options         []string
	sensitive       bool
	unique          bool
}

type tplBlock struct {
	entity, name string
	fields       []tplField
}

func templates(t string) []tplBlock {
	switch t {
	case "ent":
		return []tplBlock{
			{"student", "Анкета ЕНТ", []tplField{
				{key: "grade", typ: "select", label: "Класс", options: []string{"9", "10", "11"},
					settings: map[string]any{"required": "always", "show_in_table": true, "show_in_create": true, "show_in_teacher": true}},
				{key: "school", typ: "string", label: "Школа", settings: map[string]any{"show_in_table": true, "show_in_create": true}},
				{key: "target_score", typ: "integer", label: "Целевой балл ЕНТ",
					settings: map[string]any{"min": 0, "max": 140, "required": "on_status", "required_status": "active", "show_in_table": true, "show_in_create": true, "show_in_teacher": true}},
				{key: "trial_score", typ: "integer", label: "Пробный тест", settings: map[string]any{"min": 0, "max": 140}},
				{key: "subjects", typ: "multiselect", label: "Профильные предметы",
					options: []string{"Математика", "Физика", "Химия", "Биология", "География", "Всемирная история", "Информатика", "Английский язык"},
					settings: map[string]any{"show_in_teacher": true}},
				{key: "university", typ: "string", label: "Желаемый вуз"},
				{key: "photo_consent", typ: "boolean", label: "Согласие на фото в соцсетях"},
			}},
			{"student", "Для договора", []tplField{
				{key: "contract_number", typ: "string", label: "Номер договора", unique: true},
			}},
			{"student", "Медицинское", []tplField{
				{key: "health_notes", typ: "text", label: "Особенности здоровья", sensitive: true,
					settings: map[string]any{"roles_view": []string{"owner", "org_admin", "teacher"}, "roles_edit": []string{"owner", "org_admin"}}},
			}},
			{"parent", "Анкета", []tplField{
				{key: "workplace", typ: "string", label: "Место работы"},
				{key: "call_time", typ: "string", label: "Удобное время звонка"},
			}},
			{"group", "Параметры", []tplField{
				{key: "textbook", typ: "string", label: "Учебник"},
				{key: "zoom_link", typ: "url", label: "Ссылка на Zoom"},
			}},
			{"lead", "Анкета", []tplField{
				{key: "budget", typ: "money", label: "Бюджет"},
				{key: "grade", typ: "select", label: "Класс", options: []string{"9", "10", "11"}},
				{key: "wish_schedule", typ: "string", label: "Желаемое расписание"},
			}},
		}
	case "language":
		return []tplBlock{
			{"student", "Анкета", []tplField{
				{key: "level", typ: "select", label: "Уровень по тесту", options: []string{"A1", "A2", "B1", "B2", "C1", "C2"},
					settings: map[string]any{"show_in_table": true, "show_in_create": true, "show_in_teacher": true}},
				{key: "goal", typ: "select", label: "Цель обучения", options: []string{"IELTS", "Работа", "Путешествия", "Школа", "Эмиграция"},
					settings: map[string]any{"show_in_table": true}},
				{key: "ielts_target", typ: "decimal", label: "Целевой балл IELTS", settings: map[string]any{"min": 0, "max": 9, "precision": 1}},
			}},
			{"group", "Параметры", []tplField{{key: "textbook", typ: "string", label: "Учебник"}, {key: "zoom_link", typ: "url", label: "Ссылка на Zoom"}}},
		}
	case "kids":
		return []tplBlock{
			{"student", "Анкета", []tplField{
				{key: "kindergarten", typ: "string", label: "Садик / школа", settings: map[string]any{"show_in_table": true}},
				{key: "pickup", typ: "string", label: "Кто забирает ребёнка", settings: map[string]any{"show_in_teacher": true}},
			}},
			{"student", "Медицинское", []tplField{
				{key: "allergies", typ: "text", label: "Аллергии", sensitive: true, settings: map[string]any{"show_in_teacher": true}},
			}},
		}
	case "it":
		return []tplBlock{
			{"student", "Анкета", []tplField{
				{key: "level", typ: "select", label: "Уровень", options: []string{"Новичок", "Базовый", "Продвинутый"}, settings: map[string]any{"show_in_table": true}},
				{key: "own_laptop", typ: "boolean", label: "Есть свой ноутбук"},
				{key: "github", typ: "url", label: "GitHub"},
			}},
		}
	}
	return nil
}

func applyFieldTemplate(ctx context.Context, tx pgx.Tx, tenantID, template string) error {
	order := map[string]int{}
	for bi, b := range templates(template) {
		var blockID string
		name, _ := json.Marshal(map[string]string{"ru": b.name})
		if err := tx.QueryRow(ctx, `INSERT INTO custom_field_blocks (tenant_id, entity_type, name, sort_order) VALUES ($1,$2,$3,$4) RETURNING id`,
			tenantID, b.entity, name, bi).Scan(&blockID); err != nil {
			return err
		}
		for _, f := range b.fields {
			label, _ := json.Marshal(map[string]string{"ru": f.label})
			if f.settings == nil {
				f.settings = map[string]any{}
			}
			settings, _ := json.Marshal(f.settings)
			var fieldID string
			order[b.entity]++
			if err := tx.QueryRow(ctx, `INSERT INTO custom_field_definitions (tenant_id, entity_type, key, field_type, label, block_id, sort_order, settings, is_sensitive, is_unique)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id`,
				tenantID, b.entity, f.key, f.typ, label, blockID, order[b.entity], settings, f.sensitive, f.unique).Scan(&fieldID); err != nil {
				return err
			}
			for oi, o := range f.options {
				ol, _ := json.Marshal(map[string]string{"ru": o})
				if _, err := tx.Exec(ctx, `INSERT INTO custom_field_options (tenant_id, field_id, label, sort_order) VALUES ($1,$2,$3,$4)`,
					tenantID, fieldID, ol, oi); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
