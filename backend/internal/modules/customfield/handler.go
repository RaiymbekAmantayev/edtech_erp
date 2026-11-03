package customfield

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
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
	r.Get("/custom-fields", httpx.Handler(h.list).ServeHTTP)
	r.Get("/custom-field-blocks", httpx.Handler(h.listBlocks).ServeHTTP)
	r.Group(func(r chi.Router) {
		r.Use(mw.Perm("custom_fields.manage"))
		r.Post("/custom-fields", httpx.Handler(h.create).ServeHTTP)
		r.Patch("/custom-fields/{id}", httpx.Handler(h.update).ServeHTTP)
		r.Post("/custom-fields/{id}/archive", httpx.Handler(h.archive).ServeHTTP)
		r.Post("/custom-fields/{id}/restore", httpx.Handler(h.restore).ServeHTTP)
		r.Post("/custom-fields/{id}/purge", httpx.Handler(h.purge).ServeHTTP)
		r.Put("/custom-fields/order", httpx.Handler(h.order).ServeHTTP)
		r.Post("/custom-fields/{id}/options", httpx.Handler(h.addOption).ServeHTTP)
		r.Patch("/custom-field-options/{id}", httpx.Handler(h.updateOption).ServeHTTP)
		r.Post("/custom-field-blocks", httpx.Handler(h.createBlock).ServeHTTP)
		r.Patch("/custom-field-blocks/{id}", httpx.Handler(h.updateBlock).ServeHTTP)
	})
}

func entityParam(r *http.Request) (string, error) {
	e := r.URL.Query().Get("entity")
	if _, ok := EntityTables[e]; !ok {
		return "", httpx.BadRequest("INVALID_ENTITY", "Неизвестная сущность")
	}
	return e, nil
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) error {
	entity, err := entityParam(r)
	if err != nil {
		return err
	}
	p := auth.FromContext(r.Context())
	var fields []Field
	var limit, used int
	counts := map[string]int{}
	err = h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		manage := p.Can("custom_fields.manage")
		fields, err = Load(r.Context(), tx, entity, manage && r.URL.Query().Get("archived") == "1")
		if err != nil {
			return err
		}
		if !manage {
			fields = Visible(fields, p)
		}
		tx.QueryRow(r.Context(), `SELECT sp.max_custom_fields FROM tenants t JOIN subscription_plans sp ON sp.code=t.plan_code WHERE t.id = app_current_tenant()`).Scan(&limit)
		rows, err := tx.Query(r.Context(), `SELECT entity_type, count(*) FROM custom_field_definitions WHERE archived_at IS NULL GROUP BY entity_type`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var e string
			var c int
			rows.Scan(&e, &c)
			counts[e] = c
			used += c
		}
		return rows.Err()
	})
	if err != nil {
		return err
	}
	return httpx.OKMeta(w, fields, map[string]any{"limit_per_entity": limit, "used": counts[entity], "used_total": used, "counts": counts})
}

type fieldReq struct {
	EntityType  string             `json:"entity_type"`
	Key         string             `json:"key"`
	Type        string             `json:"field_type"`
	Label       map[string]string  `json:"label"`
	BlockID     *string            `json:"block_id"`
	Settings    *Settings          `json:"settings"`
	IsSensitive *bool              `json:"is_sensitive"`
	IsUnique    *bool              `json:"is_unique"`
	Options     []map[string]string `json:"options"` // при создании: [{"ru": "..."}]
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) error {
	var req fieldReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	ctx := r.Context()
	v := &httpx.Validation{}
	if _, ok := EntityTables[req.EntityType]; !ok {
		v.Add("entity_type", "INVALID")
	}
	if !slices.Contains(FieldTypes, req.Type) {
		v.Add("field_type", "INVALID")
	}
	if strings.TrimSpace(req.Label["ru"]) == "" {
		v.Add("label.ru", "REQUIRED")
	}
	if req.Key == "" {
		req.Key = GenerateKey(req.Label["ru"])
	}
	if !ValidKey(req.Key) {
		v.Add("key", "INVALID_KEY")
	}
	if err := v.Err(); err != nil {
		return err
	}
	if req.Settings == nil {
		req.Settings = &Settings{}
	}
	settings, _ := json.Marshal(req.Settings)
	var id string
	err := h.DB.Tx(ctx, func(tx pgx.Tx) error {
		var limit, count int
		tx.QueryRow(ctx, `SELECT sp.max_custom_fields FROM tenants t JOIN subscription_plans sp ON sp.code=t.plan_code WHERE t.id = app_current_tenant()`).Scan(&limit)
		tx.QueryRow(ctx, `SELECT count(*) FROM custom_field_definitions WHERE entity_type=$1 AND archived_at IS NULL`, req.EntityType).Scan(&count)
		if count >= limit {
			return httpx.Conflict("CUSTOM_FIELD_LIMIT", fmt.Sprintf("На вашем тарифе доступно %d полей для этой сущности", limit))
		}
		// гарантируем уникальность автоматически сгенерированного ключа
		base, key := req.Key, req.Key
		for i := 2; ; i++ {
			var exists bool
			tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM custom_field_definitions WHERE entity_type=$1 AND key=$2)`, req.EntityType, key).Scan(&exists)
			if !exists {
				break
			}
			key = fmt.Sprintf("%s_%d", base, i)
		}
		var maxOrder int
		tx.QueryRow(ctx, `SELECT coalesce(max(sort_order),0) FROM custom_field_definitions WHERE entity_type=$1`, req.EntityType).Scan(&maxOrder)
		label, _ := json.Marshal(req.Label)
		if err := tx.QueryRow(ctx, `INSERT INTO custom_field_definitions (tenant_id, entity_type, key, field_type, label, block_id, sort_order, settings, is_sensitive, is_unique)
			VALUES (app_current_tenant(),$1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`,
			req.EntityType, key, req.Type, label, req.BlockID, maxOrder+1, settings, req.IsSensitive != nil && *req.IsSensitive, req.IsUnique != nil && *req.IsUnique).Scan(&id); err != nil {
			return err
		}
		for i, o := range req.Options {
			if strings.TrimSpace(o["ru"]) == "" {
				continue
			}
			ol, _ := json.Marshal(o)
			if _, err := tx.Exec(ctx, `INSERT INTO custom_field_options (tenant_id, field_id, label, sort_order) VALUES (app_current_tenant(),$1,$2,$3)`, id, ol, i); err != nil {
				return err
			}
		}
		return database.Audit(ctx, tx, "custom_field", id, "create", nil, map[string]any{"key": key, "type": req.Type, "label": req.Label, "entity": req.EntityType})
	})
	if err != nil {
		return err
	}
	return httpx.Created(w, map[string]string{"id": id})
}

// Безопасные переходы типа (ТЗ 9.4).
var safeTypeChanges = map[string]string{"string": "text", "select": "multiselect"}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	var req fieldReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	ctx := r.Context()
	return h.DB.Tx(ctx, func(tx pgx.Tx) error {
		var entity, curType string
		var oldLabel map[string]string
		var oldSettings []byte
		if err := tx.QueryRow(ctx, `SELECT entity_type, field_type, label, settings FROM custom_field_definitions WHERE id=$1`, id).
			Scan(&entity, &curType, &oldLabel, &oldSettings); err != nil {
			return err
		}
		if req.Type != "" && req.Type != curType {
			if safeTypeChanges[curType] != req.Type {
				return httpx.BadRequest("FIELD_TYPE_IMMUTABLE", "Тип поля после создания не меняется")
			}
			if curType == "select" {
				// одно значение → массив из одного варианта
				var key string
				tx.QueryRow(ctx, `SELECT key FROM custom_field_definitions WHERE id=$1`, id).Scan(&key)
				q := fmt.Sprintf(`UPDATE %s SET custom_fields = jsonb_set(custom_fields, ARRAY[$1], jsonb_build_array(custom_fields->$1)) WHERE jsonb_typeof(custom_fields->$1)='string'`,
					pgx.Identifier{EntityTables[entity]}.Sanitize())
				if _, err := tx.Exec(ctx, q, key); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(ctx, `UPDATE custom_field_definitions SET field_type=$2 WHERE id=$1`, id, req.Type); err != nil {
				return err
			}
		}
		if req.Label != nil {
			if strings.TrimSpace(req.Label["ru"]) == "" {
				return (&httpx.Validation{Fields: []httpx.FieldError{{Field: "label.ru", Code: "REQUIRED"}}}).Err()
			}
			label, _ := json.Marshal(req.Label)
			if _, err := tx.Exec(ctx, `UPDATE custom_field_definitions SET label=$2 WHERE id=$1`, id, label); err != nil {
				return err
			}
		}
		if req.BlockID != nil {
			if _, err := tx.Exec(ctx, `UPDATE custom_field_definitions SET block_id=NULLIF($2,'')::uuid WHERE id=$1`, id, *req.BlockID); err != nil {
				return err
			}
		}
		if req.Settings != nil {
			s, _ := json.Marshal(req.Settings)
			if _, err := tx.Exec(ctx, `UPDATE custom_field_definitions SET settings=$2 WHERE id=$1`, id, s); err != nil {
				return err
			}
		}
		if req.IsSensitive != nil {
			tx.Exec(ctx, `UPDATE custom_field_definitions SET is_sensitive=$2 WHERE id=$1`, id, *req.IsSensitive)
		}
		if req.IsUnique != nil {
			tx.Exec(ctx, `UPDATE custom_field_definitions SET is_unique=$2 WHERE id=$1`, id, *req.IsUnique)
		}
		return database.Audit(ctx, tx, "custom_field", id, "update", map[string]any{"label": oldLabel, "settings": json.RawMessage(oldSettings)}, req)
	})
}

func (h *Handler) archive(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	return h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), `UPDATE custom_field_definitions SET archived_at=NOW() WHERE id=$1 AND archived_at IS NULL`, id); err != nil {
			return err
		}
		return database.Audit(r.Context(), tx, "custom_field", id, "archive", nil, nil)
	})
}

func (h *Handler) restore(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	return h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), `UPDATE custom_field_definitions SET archived_at=NULL WHERE id=$1`, id); err != nil {
			return err
		}
		return database.Audit(r.Context(), tx, "custom_field", id, "restore", nil, nil)
	})
}

// purge — физическое удаление данных поля: только Owner и не раньше 30 дней после архивации (ТЗ 9.5).
func (h *Handler) purge(w http.ResponseWriter, r *http.Request) error {
	p := auth.FromContext(r.Context())
	if !p.HasRole("owner") {
		return httpx.Forbidden()
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	return h.DB.Tx(ctx, func(tx pgx.Tx) error {
		var entity, key string
		var archived *time.Time
		if err := tx.QueryRow(ctx, `SELECT entity_type, key, archived_at FROM custom_field_definitions WHERE id=$1`, id).Scan(&entity, &key, &archived); err != nil {
			return err
		}
		if archived == nil || time.Since(*archived) < 30*24*time.Hour {
			return httpx.Conflict("PURGE_TOO_EARLY", "Удалить данные можно через 30 дней после архивации поля")
		}
		q := fmt.Sprintf(`UPDATE %s SET custom_fields = custom_fields - $1 WHERE custom_fields ? $1`, pgx.Identifier{EntityTables[entity]}.Sanitize())
		if _, err := tx.Exec(ctx, q, key); err != nil {
			return err
		}
		tx.Exec(ctx, `DELETE FROM custom_field_options WHERE field_id=$1`, id)
		if _, err := tx.Exec(ctx, `DELETE FROM custom_field_definitions WHERE id=$1`, id); err != nil {
			return err
		}
		return database.Audit(ctx, tx, "custom_field", id, "purge", map[string]any{"key": key, "entity": entity}, nil)
	})
}

func (h *Handler) order(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Items []struct {
			ID      string  `json:"id"`
			BlockID *string `json:"block_id"`
		} `json:"items"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	return h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		for i, it := range req.Items {
			if _, err := tx.Exec(r.Context(), `UPDATE custom_field_definitions SET sort_order=$2, block_id=COALESCE($3::uuid, block_id) WHERE id=$1`, it.ID, i+1, it.BlockID); err != nil {
				return err
			}
		}
		return nil
	})
}

func (h *Handler) addOption(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Label map[string]string `json:"label"`
		Color *string           `json:"color"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if strings.TrimSpace(req.Label["ru"]) == "" {
		return (&httpx.Validation{Fields: []httpx.FieldError{{Field: "label.ru", Code: "REQUIRED"}}}).Err()
	}
	fid := chi.URLParam(r, "id")
	var id string
	err := h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		var count int
		tx.QueryRow(r.Context(), `SELECT count(*) FROM custom_field_options WHERE field_id=$1`, fid).Scan(&count)
		if count >= 500 {
			return httpx.Conflict("OPTIONS_LIMIT", "Не больше 500 вариантов в списке")
		}
		l, _ := json.Marshal(req.Label)
		return tx.QueryRow(r.Context(), `INSERT INTO custom_field_options (tenant_id, field_id, label, color, sort_order) VALUES (app_current_tenant(),$1,$2,$3,$4) RETURNING id`,
			fid, l, req.Color, count).Scan(&id)
	})
	if err != nil {
		return err
	}
	return httpx.Created(w, map[string]string{"id": id})
}

// updateOption — переименование или архивация варианта. Данные хранятся по ID, поэтому переименование ничего не ломает.
func (h *Handler) updateOption(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Label    map[string]string `json:"label"`
		Color    *string           `json:"color"`
		Archived *bool             `json:"archived"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	id := chi.URLParam(r, "id")
	return h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		if req.Label != nil {
			l, _ := json.Marshal(req.Label)
			tx.Exec(r.Context(), `UPDATE custom_field_options SET label=$2 WHERE id=$1`, id, l)
		}
		if req.Color != nil {
			tx.Exec(r.Context(), `UPDATE custom_field_options SET color=$2 WHERE id=$1`, id, req.Color)
		}
		if req.Archived != nil {
			tx.Exec(r.Context(), `UPDATE custom_field_options SET archived_at=CASE WHEN $2 THEN NOW() END WHERE id=$1`, id, *req.Archived)
		}
		return nil
	})
}

type blockDTO struct {
	ID         string            `json:"id"`
	EntityType string            `json:"entity_type"`
	Name       map[string]string `json:"name"`
	SortOrder  int               `json:"sort_order"`
}

func (h *Handler) listBlocks(w http.ResponseWriter, r *http.Request) error {
	entity, err := entityParam(r)
	if err != nil {
		return err
	}
	out := []blockDTO{}
	err = h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `SELECT id, entity_type, name, sort_order FROM custom_field_blocks WHERE entity_type=$1 ORDER BY sort_order`, entity)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var b blockDTO
			rows.Scan(&b.ID, &b.EntityType, &b.Name, &b.SortOrder)
			out = append(out, b)
		}
		return rows.Err()
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, out)
}

func (h *Handler) createBlock(w http.ResponseWriter, r *http.Request) error {
	var req blockDTO
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if _, ok := EntityTables[req.EntityType]; !ok || strings.TrimSpace(req.Name["ru"]) == "" {
		return httpx.BadRequest("VALIDATION_FAILED", "Укажите сущность и название блока")
	}
	var id string
	err := h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		n, _ := json.Marshal(req.Name)
		return tx.QueryRow(r.Context(), `INSERT INTO custom_field_blocks (tenant_id, entity_type, name, sort_order)
			VALUES (app_current_tenant(),$1,$2,(SELECT coalesce(max(sort_order),0)+1 FROM custom_field_blocks WHERE entity_type=$1)) RETURNING id`,
			req.EntityType, n).Scan(&id)
	})
	if err != nil {
		return err
	}
	return httpx.Created(w, map[string]string{"id": id})
}

func (h *Handler) updateBlock(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Name      map[string]string `json:"name"`
		SortOrder *int              `json:"sort_order"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	id := chi.URLParam(r, "id")
	return h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		if req.Name != nil {
			n, _ := json.Marshal(req.Name)
			tx.Exec(r.Context(), `UPDATE custom_field_blocks SET name=$2 WHERE id=$1`, id, n)
		}
		if req.SortOrder != nil {
			tx.Exec(r.Context(), `UPDATE custom_field_blocks SET sort_order=$2 WHERE id=$1`, id, *req.SortOrder)
		}
		return nil
	})
}
