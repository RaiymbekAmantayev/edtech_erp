// Пакет ktp — календарно-тематический план школы: категории (по предмету, уровню, классу — как удобно школе),
// темы уроков и домашние задания. Тема и ДЗ выбираются при отметке занятия.
package ktp

import (
	"context"
	"net/http"
	"strings"

	"emp/internal/modules/exam"
	"emp/internal/platform/database"
	"emp/internal/platform/httpx"
	mw "emp/internal/platform/middleware"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

type Handler struct{ DB *database.DB }

func (h *Handler) Routes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(mw.Perm("settings.manage"))
		r.Get("/ktp", httpx.Handler(h.list).ServeHTTP)
		r.Post("/ktp/categories", httpx.Handler(h.createCategory).ServeHTTP)
		r.Patch("/ktp/categories/{id}", httpx.Handler(h.updateCategory).ServeHTTP)
		r.Post("/ktp/categories/{id}/topics", httpx.Handler(h.addTopics).ServeHTTP)
		r.Put("/ktp/categories/{id}/order", httpx.Handler(h.order).ServeHTTP)
		r.Patch("/ktp/topics/{id}", httpx.Handler(h.updateTopic).ServeHTTP)
		r.Delete("/ktp/topics/{id}", httpx.Handler(h.deleteTopic).ServeHTTP)
	})
}

type Topic struct {
	ID       string   `json:"id"`
	Number   int      `json:"number"`
	Title    string   `json:"title"`
	Homework *string  `json:"homework"`
	Hours    *float64 `json:"hours"`
	Done     bool     `json:"done,omitempty"` // тема уже пройдена в этой группе
}

type Category struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	CourseIDs []string `json:"course_ids"`
	Archived  bool     `json:"archived"`
	Topics    []Topic  `json:"topics"`
}

// load читает категории с темами. where фильтрует категории (алиас c), groupID — отмечает пройденные темы.
func load(ctx context.Context, tx pgx.Tx, where string, args []any, groupID string) ([]Category, error) {
	out := []Category{}
	rows, err := tx.Query(ctx, `SELECT c.id::text, c.name, c.course_ids::text[], c.archived_at IS NOT NULL FROM ktp_categories c
		WHERE `+where+` ORDER BY c.archived_at NULLS FIRST, c.sort_order, c.name`, args...)
	if err != nil {
		return nil, err
	}
	idx := map[string]int{}
	ids := []string{}
	for rows.Next() {
		c := Category{Topics: []Topic{}}
		if err := rows.Scan(&c.ID, &c.Name, &c.CourseIDs, &c.Archived); err != nil {
			rows.Close()
			return nil, err
		}
		idx[c.ID] = len(out)
		ids = append(ids, c.ID)
		out = append(out, c)
	}
	rows.Close()
	if len(ids) == 0 {
		return out, rows.Err()
	}
	rows, err = tx.Query(ctx, `SELECT t.category_id::text, t.id::text, t.title, t.homework, t.hours::float8,
			$2 <> '' AND EXISTS (SELECT 1 FROM lessons l WHERE l.ktp_topic_id=t.id AND l.group_id::text=$2 AND l.status='done')
		FROM ktp_topics t WHERE t.category_id = ANY($1::uuid[]) ORDER BY t.sort_order, t.created_at`, ids, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid string
		var t Topic
		if err := rows.Scan(&cid, &t.ID, &t.Title, &t.Homework, &t.Hours, &t.Done); err != nil {
			return nil, err
		}
		c := &out[idx[cid]]
		t.Number = len(c.Topics) + 1
		c.Topics = append(c.Topics, t)
	}
	return out, rows.Err()
}

// ForLesson — категории КТП для занятия группы: привязанные к курсу группы, а если таких нет — все действующие.
// suggested — первая непройденная тема (следующая по плану).
func ForLesson(ctx context.Context, tx pgx.Tx, groupID, courseID string) (cats []Category, suggested *string, err error) {
	cats, err = load(ctx, tx, `c.archived_at IS NULL AND $1::uuid = ANY(c.course_ids)`, []any{courseID}, groupID)
	if err != nil {
		return nil, nil, err
	}
	if len(cats) == 0 {
		if cats, err = load(ctx, tx, `c.archived_at IS NULL`, nil, groupID); err != nil {
			return nil, nil, err
		}
	}
	if len(cats) > 0 {
		for _, t := range cats[0].Topics {
			if !t.Done {
				id := t.ID
				suggested = &id
				break
			}
		}
	}
	return cats, suggested, nil
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) error {
	var out []Category
	err := h.DB.Tx(r.Context(), func(tx pgx.Tx) (err error) {
		out, err = load(r.Context(), tx, `($1 OR c.archived_at IS NULL)`, []any{r.URL.Query().Get("archived") == "1"}, "")
		return err
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, out)
}

type categoryReq struct {
	Name      *string   `json:"name"`
	CourseIDs *[]string `json:"course_ids"`
	Archived  *bool     `json:"archived"`
}

func (c categoryReq) validate(create bool) error {
	v := &httpx.Validation{}
	if create || c.Name != nil {
		if c.Name == nil || strings.TrimSpace(*c.Name) == "" {
			v.Add("name", "REQUIRED")
		} else if len([]rune(strings.TrimSpace(*c.Name))) > 160 {
			v.Add("name", "TOO_LONG")
		}
	}
	return v.Err()
}

func (h *Handler) createCategory(w http.ResponseWriter, r *http.Request) error {
	var req categoryReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if err := req.validate(true); err != nil {
		return err
	}
	courses := []string{}
	if req.CourseIDs != nil {
		courses = *req.CourseIDs
	}
	ctx := r.Context()
	var id string
	err := h.DB.Tx(ctx, func(tx pgx.Tx) error {
		if err := exam.CheckCourses(ctx, tx, courses); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `INSERT INTO ktp_categories (tenant_id, name, course_ids, sort_order)
			VALUES (app_current_tenant(), $1, $2::uuid[], (SELECT coalesce(max(sort_order),0)+1 FROM ktp_categories)) RETURNING id`,
			strings.TrimSpace(*req.Name), courses).Scan(&id); err != nil {
			return err
		}
		return database.Audit(ctx, tx, "ktp_category", id, "create", nil, req)
	})
	if err != nil {
		return err
	}
	return httpx.Created(w, map[string]string{"id": id})
}

func (h *Handler) updateCategory(w http.ResponseWriter, r *http.Request) error {
	var req categoryReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if err := req.validate(false); err != nil {
		return err
	}
	if req.Name != nil {
		*req.Name = strings.TrimSpace(*req.Name)
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	return h.DB.Tx(ctx, func(tx pgx.Tx) error {
		var courses []string
		if req.CourseIDs != nil {
			courses = *req.CourseIDs
			if courses == nil {
				courses = []string{}
			}
			if err := exam.CheckCourses(ctx, tx, courses); err != nil {
				return err
			}
		}
		tag, err := tx.Exec(ctx, `UPDATE ktp_categories SET name=coalesce($2, name), course_ids=coalesce($3::uuid[], course_ids),
			archived_at=CASE WHEN $4::bool IS NULL THEN archived_at WHEN $4 THEN coalesce(archived_at, NOW()) END WHERE id=$1`,
			id, req.Name, courses, req.Archived)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		return database.Audit(ctx, tx, "ktp_category", id, "update", nil, req)
	})
}

type topicReq struct {
	Title    *string  `json:"title"`
	Homework *string  `json:"homework"`
	Hours    *float64 `json:"hours"`
}

func (t topicReq) validate(create bool) error {
	v := &httpx.Validation{}
	if create || t.Title != nil {
		if t.Title == nil || strings.TrimSpace(*t.Title) == "" {
			v.Add("title", "REQUIRED")
		} else if len([]rune(strings.TrimSpace(*t.Title))) > 500 {
			v.Add("title", "TOO_LONG")
		}
	}
	if t.Homework != nil && len([]rune(*t.Homework)) > 5000 {
		v.Add("homework", "TOO_LONG")
	}
	if t.Hours != nil && (*t.Hours < 0 || *t.Hours > 1000) {
		v.Add("hours", "INVALID")
	}
	return v.Err()
}

// addTopics добавляет одну тему или сразу список (вставка КТП из таблицы) в конец категории.
func (h *Handler) addTopics(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		topicReq
		Items []topicReq `json:"items"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	items := req.Items
	if len(items) == 0 {
		items = []topicReq{req.topicReq}
	}
	if len(items) > 500 {
		return (&httpx.Validation{Fields: []httpx.FieldError{{Field: "items", Code: "TOO_LARGE"}}}).Err()
	}
	for _, it := range items {
		if err := it.validate(true); err != nil {
			return err
		}
	}
	cid := chi.URLParam(r, "id")
	ctx := r.Context()
	ids := []string{}
	err := h.DB.Tx(ctx, func(tx pgx.Tx) error {
		var max int
		if err := tx.QueryRow(ctx, `SELECT coalesce((SELECT max(sort_order) FROM ktp_topics WHERE category_id=c.id), 0) FROM ktp_categories c WHERE c.id=$1`, cid).Scan(&max); err != nil {
			return err
		}
		for i, it := range items {
			var id string
			if err := tx.QueryRow(ctx, `INSERT INTO ktp_topics (tenant_id, category_id, title, homework, hours, sort_order)
				VALUES (app_current_tenant(), $1, $2, NULLIF(trim($3),''), $4, $5) RETURNING id`,
				cid, strings.TrimSpace(*it.Title), it.Homework, it.Hours, max+i+1).Scan(&id); err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return database.Audit(ctx, tx, "ktp_category", cid, "topics_added", nil, map[string]any{"count": len(ids)})
	})
	if err != nil {
		return err
	}
	return httpx.Created(w, map[string]any{"ids": ids})
}

func (h *Handler) updateTopic(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		topicReq
		ClearHours bool `json:"clear_hours"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if err := req.validate(false); err != nil {
		return err
	}
	if req.Title != nil {
		*req.Title = strings.TrimSpace(*req.Title)
	}
	id := chi.URLParam(r, "id")
	return h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		tag, err := tx.Exec(r.Context(), `UPDATE ktp_topics SET title=coalesce($2, title),
			homework=CASE WHEN $3::text IS NULL THEN homework ELSE NULLIF(trim($3),'') END,
			hours=CASE WHEN $5 THEN NULL ELSE coalesce($4, hours) END WHERE id=$1`, id, req.Title, req.Homework, req.Hours, req.ClearHours)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		return nil
	})
}

// deleteTopic: проведённые занятия сохраняют свою тему и ДЗ текстом, теряется только ссылка на КТП.
func (h *Handler) deleteTopic(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	err := h.DB.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE lessons SET ktp_topic_id=NULL WHERE ktp_topic_id=$1`, id); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `DELETE FROM ktp_topics WHERE id=$1`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		return nil
	})
	if err != nil {
		return err
	}
	return httpx.NoContent(w)
}

func (h *Handler) order(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	cid := chi.URLParam(r, "id")
	return h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		for i, id := range req.IDs {
			if _, err := tx.Exec(r.Context(), `UPDATE ktp_topics SET sort_order=$3 WHERE id=$1 AND category_id=$2`, id, cid, i+1); err != nil {
				return err
			}
		}
		return nil
	})
}
