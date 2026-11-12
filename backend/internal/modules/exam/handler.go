// Пакет exam — тестирование учеников: школа настраивает типы тестов, навыки и шкалу оценок,
// преподаватели вносят результаты (в том числе из Telegram Mini App).
package exam

import (
	"context"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"emp/internal/platform/auth"
	"emp/internal/platform/database"
	"emp/internal/platform/httpx"
	mw "emp/internal/platform/middleware"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/xuri/excelize/v2"
)

type Handler struct{ DB *database.DB }

func (h *Handler) Routes(r chi.Router) {
	r.Get("/tests/config", httpx.Handler(h.config).ServeHTTP)
	r.Group(func(r chi.Router) {
		r.Use(mw.Perm("settings.manage"))
		r.Post("/test-skills", httpx.Handler(h.createSkill).ServeHTTP)
		r.Patch("/test-skills/{id}", httpx.Handler(h.updateSkill).ServeHTTP)
		r.Post("/test-grades", httpx.Handler(h.createGrade).ServeHTTP)
		r.Patch("/test-grades/{id}", httpx.Handler(h.updateGrade).ServeHTTP)
		r.Post("/test-types", httpx.Handler(h.createType).ServeHTTP)
		r.Patch("/test-types/{id}", httpx.Handler(h.updateType).ServeHTTP)
	})
	r.With(mw.Perm("students.view", "tests.conduct")).Get("/tests", httpx.Handler(h.list).ServeHTTP)
	r.Group(func(r chi.Router) {
		r.Use(mw.Perm("tests.conduct"))
		r.Post("/tests", httpx.Handler(h.create).ServeHTTP)
		r.Delete("/tests/{id}", httpx.Handler(h.remove).ServeHTTP)
	})
}

// isStaff — администратор/учебный отдел (видит учеников филиалов); иначе — преподаватель, которому доступны только свои группы.
func isStaff(p *auth.Principal) bool { return p.Can("students.view") }

// ---------------------------------------------------------------- справочники

type Skill struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Archived bool   `json:"archived"`
}

type Grade struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Color      *string  `json:"color"`
	MinPercent *float64 `json:"min_percent"`
	Archived   bool     `json:"archived"`
}

type TypeSkill struct {
	SkillID  string  `json:"skill_id"`
	Name     string  `json:"name"`
	MaxScore float64 `json:"max_score"`
}

type Type struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Description *string     `json:"description"`
	Scoring     string      `json:"scoring"`
	LessonMode  string      `json:"lesson_mode"`
	CourseIDs   []string    `json:"course_ids"`
	Archived    bool        `json:"archived"`
	Skills      []TypeSkill `json:"skills"`
	Results     int         `json:"results"`
}

type Config struct {
	Types  []Type  `json:"types"`
	Skills []Skill `json:"skills"`
	Grades []Grade `json:"grades"`
}

// LoadConfig читает настройки тестирования школы. lessonOnly — только тесты, которые вводятся на уроке.
func LoadConfig(ctx context.Context, tx pgx.Tx, archived, lessonOnly bool) (Config, error) {
	c := Config{Types: []Type{}, Skills: []Skill{}, Grades: []Grade{}}
	rows, err := tx.Query(ctx, `SELECT id, name, archived_at IS NOT NULL FROM test_skills WHERE ($1 OR archived_at IS NULL) ORDER BY archived_at NULLS FIRST, sort_order, name`, archived)
	if err != nil {
		return c, err
	}
	for rows.Next() {
		var s Skill
		if err := rows.Scan(&s.ID, &s.Name, &s.Archived); err != nil {
			return c, err
		}
		c.Skills = append(c.Skills, s)
	}
	rows.Close()
	rows, err = tx.Query(ctx, `SELECT id, name, color, min_percent::float8, archived_at IS NOT NULL FROM test_grades WHERE ($1 OR archived_at IS NULL)
		ORDER BY archived_at NULLS FIRST, min_percent DESC NULLS LAST, sort_order, name`, archived)
	if err != nil {
		return c, err
	}
	for rows.Next() {
		var g Grade
		if err := rows.Scan(&g.ID, &g.Name, &g.Color, &g.MinPercent, &g.Archived); err != nil {
			return c, err
		}
		c.Grades = append(c.Grades, g)
	}
	rows.Close()
	rows, err = tx.Query(ctx, `SELECT t.id, t.name, t.description, t.scoring, t.lesson_mode, t.course_ids::text[], t.archived_at IS NOT NULL,
			(SELECT count(*) FROM student_tests st WHERE st.test_type_id=t.id)
		FROM test_types t WHERE ($1 OR t.archived_at IS NULL) AND (NOT $2 OR t.lesson_mode <> 'off')
		ORDER BY t.archived_at NULLS FIRST, t.sort_order, t.name`, archived, lessonOnly)
	if err != nil {
		return c, err
	}
	idx := map[string]int{}
	for rows.Next() {
		t := Type{Skills: []TypeSkill{}}
		if err := rows.Scan(&t.ID, &t.Name, &t.Description, &t.Scoring, &t.LessonMode, &t.CourseIDs, &t.Archived, &t.Results); err != nil {
			return c, err
		}
		idx[t.ID] = len(c.Types)
		c.Types = append(c.Types, t)
	}
	rows.Close()
	rows, err = tx.Query(ctx, `SELECT ts.test_type_id, ts.skill_id, s.name, ts.max_score::float8 FROM test_type_skills ts
		JOIN test_skills s ON s.id=ts.skill_id ORDER BY ts.sort_order, s.name`)
	if err != nil {
		return c, err
	}
	defer rows.Close()
	for rows.Next() {
		var typeID string
		var s TypeSkill
		if err := rows.Scan(&typeID, &s.SkillID, &s.Name, &s.MaxScore); err != nil {
			return c, err
		}
		if i, ok := idx[typeID]; ok {
			c.Types[i].Skills = append(c.Types[i].Skills, s)
		}
	}
	return c, rows.Err()
}

func (h *Handler) config(w http.ResponseWriter, r *http.Request) error {
	p := auth.FromContext(r.Context())
	manage := p.Can("settings.manage")
	// преподаватель без доступа к карточкам учеников видит только тесты, которые вводятся на уроке
	lessonOnly := r.URL.Query().Get("lesson") == "1" || (!manage && !isStaff(p))
	var c Config
	err := h.DB.Tx(r.Context(), func(tx pgx.Tx) (err error) {
		c, err = LoadConfig(r.Context(), tx, manage && r.URL.Query().Get("archived") == "1", lessonOnly)
		return err
	})
	if err != nil {
		return err
	}
	return httpx.OK(w, c)
}

func nameRequired(name string, max int) error {
	n := strings.TrimSpace(name)
	v := &httpx.Validation{}
	if n == "" {
		v.Add("name", "REQUIRED")
	} else if len([]rune(n)) > max {
		v.Add("name", "TOO_LONG")
	}
	return v.Err()
}

func (h *Handler) createSkill(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Name string `json:"name"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if err := nameRequired(req.Name, 120); err != nil {
		return err
	}
	var id string
	err := h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		if err := tx.QueryRow(r.Context(), `INSERT INTO test_skills (tenant_id, name, sort_order)
			VALUES (app_current_tenant(), $1, (SELECT coalesce(max(sort_order),0)+1 FROM test_skills)) RETURNING id`, strings.TrimSpace(req.Name)).Scan(&id); err != nil {
			return err
		}
		return database.Audit(r.Context(), tx, "test_skill", id, "create", nil, req)
	})
	if err != nil {
		return err
	}
	return httpx.Created(w, map[string]string{"id": id})
}

func (h *Handler) updateSkill(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Name     *string `json:"name"`
		Archived *bool   `json:"archived"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.Name != nil {
		if err := nameRequired(*req.Name, 120); err != nil {
			return err
		}
		*req.Name = strings.TrimSpace(*req.Name)
	}
	id := chi.URLParam(r, "id")
	return h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		tag, err := tx.Exec(r.Context(), `UPDATE test_skills SET name=coalesce($2, name),
			archived_at=CASE WHEN $3::bool IS NULL THEN archived_at WHEN $3 THEN coalesce(archived_at, NOW()) END WHERE id=$1`, id, req.Name, req.Archived)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		return database.Audit(r.Context(), tx, "test_skill", id, "update", nil, req)
	})
}

type gradeReq struct {
	Name       *string  `json:"name"`
	Color      *string  `json:"color"`
	MinPercent *float64 `json:"min_percent"`
	ClearMin   bool     `json:"clear_min_percent"`
	Archived   *bool    `json:"archived"`
}

func (g gradeReq) validate(create bool) error {
	v := &httpx.Validation{}
	if create || g.Name != nil {
		if g.Name == nil || strings.TrimSpace(*g.Name) == "" {
			v.Add("name", "REQUIRED")
		} else if len([]rune(strings.TrimSpace(*g.Name))) > 60 {
			v.Add("name", "TOO_LONG")
		}
	}
	if g.MinPercent != nil && (*g.MinPercent < 0 || *g.MinPercent > 100) {
		v.Add("min_percent", "INVALID")
	}
	if g.Color != nil && len(*g.Color) > 16 {
		v.Add("color", "INVALID")
	}
	return v.Err()
}

func (h *Handler) createGrade(w http.ResponseWriter, r *http.Request) error {
	var req gradeReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if err := req.validate(true); err != nil {
		return err
	}
	var id string
	err := h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		if err := tx.QueryRow(r.Context(), `INSERT INTO test_grades (tenant_id, name, color, min_percent, sort_order)
			VALUES (app_current_tenant(), $1, NULLIF($2,''), $3, (SELECT coalesce(max(sort_order),0)+1 FROM test_grades)) RETURNING id`,
			strings.TrimSpace(*req.Name), req.Color, req.MinPercent).Scan(&id); err != nil {
			return err
		}
		return database.Audit(r.Context(), tx, "test_grade", id, "create", nil, req)
	})
	if err != nil {
		return err
	}
	return httpx.Created(w, map[string]string{"id": id})
}

func (h *Handler) updateGrade(w http.ResponseWriter, r *http.Request) error {
	var req gradeReq
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
	return h.DB.Tx(r.Context(), func(tx pgx.Tx) error {
		tag, err := tx.Exec(r.Context(), `UPDATE test_grades SET name=coalesce($2, name), color=CASE WHEN $3::text IS NULL THEN color ELSE NULLIF($3,'') END,
			min_percent=CASE WHEN $5 THEN NULL ELSE coalesce($4, min_percent) END,
			archived_at=CASE WHEN $6::bool IS NULL THEN archived_at WHEN $6 THEN coalesce(archived_at, NOW()) END WHERE id=$1`,
			id, req.Name, req.Color, req.MinPercent, req.ClearMin, req.Archived)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		return database.Audit(r.Context(), tx, "test_grade", id, "update", nil, req)
	})
}

type typeReq struct {
	Name        *string   `json:"name"`
	Description *string   `json:"description"`
	Scoring     *string   `json:"scoring"`
	LessonMode  *string   `json:"lesson_mode"`
	CourseIDs   *[]string `json:"course_ids"`
	Archived    *bool     `json:"archived"`
	Skills      *[]struct {
		SkillID  string  `json:"skill_id"`
		MaxScore float64 `json:"max_score"`
	} `json:"skills"`
}

var lessonModes = []string{"off", "optional", "required"}

func (t typeReq) validate(create bool) error {
	v := &httpx.Validation{}
	if create || t.Name != nil {
		if t.Name == nil || strings.TrimSpace(*t.Name) == "" {
			v.Add("name", "REQUIRED")
		} else if len([]rune(strings.TrimSpace(*t.Name))) > 160 {
			v.Add("name", "TOO_LONG")
		}
	}
	if t.Scoring != nil && *t.Scoring != "score" && *t.Scoring != "grade" {
		v.Add("scoring", "INVALID")
	}
	if t.LessonMode != nil && !slices.Contains(lessonModes, *t.LessonMode) {
		v.Add("lesson_mode", "INVALID")
	}
	if create && (t.Skills == nil || len(*t.Skills) == 0) {
		v.Add("skills", "REQUIRED")
	}
	if t.Skills != nil {
		if len(*t.Skills) == 0 {
			v.Add("skills", "REQUIRED")
		}
		seen := map[string]bool{}
		for _, s := range *t.Skills {
			if s.SkillID == "" || seen[s.SkillID] {
				v.Add("skills", "INVALID")
				break
			}
			seen[s.SkillID] = true
			if s.MaxScore <= 0 || s.MaxScore > 100000 {
				v.Add("skills.max_score", "INVALID")
				break
			}
		}
	}
	return v.Err()
}

func (t typeReq) saveSkills(ctx context.Context, tx pgx.Tx, id string) error {
	if t.Skills == nil {
		return nil
	}
	if _, err := tx.Exec(ctx, `DELETE FROM test_type_skills WHERE test_type_id=$1`, id); err != nil {
		return err
	}
	for i, s := range *t.Skills {
		var ok bool
		tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM test_skills WHERE id=$1 AND archived_at IS NULL)`, s.SkillID).Scan(&ok)
		if !ok {
			return (&httpx.Validation{Fields: []httpx.FieldError{{Field: "skills", Code: "INVALID_OPTION"}}}).Err()
		}
		if _, err := tx.Exec(ctx, `INSERT INTO test_type_skills (tenant_id, test_type_id, skill_id, max_score, sort_order) VALUES (app_current_tenant(), $1, $2, $3, $4)`,
			id, s.SkillID, s.MaxScore, i); err != nil {
			return err
		}
	}
	return nil
}

// CheckCourses — курсы должны существовать в школе (RLS скрывает чужие).
func CheckCourses(ctx context.Context, tx pgx.Tx, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(DISTINCT id) FROM courses WHERE id = ANY($1::uuid[])`, ids).Scan(&n); err != nil {
		return err
	}
	if n != len(ids) {
		return (&httpx.Validation{Fields: []httpx.FieldError{{Field: "course_ids", Code: "INVALID_OPTION"}}}).Err()
	}
	return nil
}

func (h *Handler) createType(w http.ResponseWriter, r *http.Request) error {
	var req typeReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if err := req.validate(true); err != nil {
		return err
	}
	scoring, mode, courses := "score", "optional", []string{}
	if req.Scoring != nil {
		scoring = *req.Scoring
	}
	if req.LessonMode != nil {
		mode = *req.LessonMode
	}
	if req.CourseIDs != nil {
		courses = *req.CourseIDs
	}
	ctx := r.Context()
	var id string
	err := h.DB.Tx(ctx, func(tx pgx.Tx) error {
		if err := CheckCourses(ctx, tx, courses); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `INSERT INTO test_types (tenant_id, name, description, scoring, lesson_mode, course_ids, sort_order)
			VALUES (app_current_tenant(), $1, NULLIF(trim($2),''), $3, $4, $5::uuid[], (SELECT coalesce(max(sort_order),0)+1 FROM test_types)) RETURNING id`,
			strings.TrimSpace(*req.Name), req.Description, scoring, mode, courses).Scan(&id); err != nil {
			return err
		}
		if err := req.saveSkills(ctx, tx, id); err != nil {
			return err
		}
		return database.Audit(ctx, tx, "test_type", id, "create", nil, req)
	})
	if err != nil {
		return err
	}
	return httpx.Created(w, map[string]string{"id": id})
}

func (h *Handler) updateType(w http.ResponseWriter, r *http.Request) error {
	var req typeReq
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
		var scoring string
		var results int
		if err := tx.QueryRow(ctx, `SELECT scoring, (SELECT count(*) FROM student_tests WHERE test_type_id=t.id) FROM test_types t WHERE id=$1 FOR UPDATE`, id).
			Scan(&scoring, &results); err != nil {
			return err
		}
		if req.Scoring != nil && *req.Scoring != scoring && results > 0 {
			return httpx.Conflict("TEST_SCORING_LOCKED", "Способ оценки нельзя изменить: по этому тесту уже есть результаты")
		}
		var courses []string
		if req.CourseIDs != nil {
			courses = *req.CourseIDs
			if courses == nil {
				courses = []string{}
			}
			if err := CheckCourses(ctx, tx, courses); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE test_types SET name=coalesce($2, name),
				description=CASE WHEN $3::text IS NULL THEN description ELSE NULLIF(trim($3),'') END,
				scoring=coalesce($4, scoring), lesson_mode=coalesce($5, lesson_mode), course_ids=coalesce($6::uuid[], course_ids),
				archived_at=CASE WHEN $7::bool IS NULL THEN archived_at WHEN $7 THEN coalesce(archived_at, NOW()) END
			WHERE id=$1`, id, req.Name, req.Description, req.Scoring, req.LessonMode, courses, req.Archived); err != nil {
			return err
		}
		if err := req.saveSkills(ctx, tx, id); err != nil {
			return err
		}
		return database.Audit(ctx, tx, "test_type", id, "update", nil, req)
	})
}

// ---------------------------------------------------------------- расчёт результата

type ScoreIn struct {
	SkillID string   `json:"skill_id"`
	Score   *float64 `json:"score"`
	GradeID *string  `json:"grade_id"`
}

type itemIn struct {
	StudentID string    `json:"student_id"`
	Scores    []ScoreIn `json:"scores"`
	GradeID   *string   `json:"grade_id"`
	Comment   *string   `json:"comment"`
}

func round2(f float64) float64 { return math.Round(f*100) / 100 }

// scorer считает итог по баллам навыков и подбирает оценку по порогам шкалы.
type scorer struct {
	scoring string
	skills  map[string]float64
	grades  map[string]bool
	auto    []autoGrade
}

type autoGrade struct {
	id  string
	min float64
}

type scoreRow struct {
	skill        string
	score, maxSc *float64
	grade        *string
}

type computed struct {
	rows                []scoreRow
	total, max, percent *float64
	gradeID             *string
}

func loadScorer(ctx context.Context, tx pgx.Tx, typeID string) (*scorer, error) {
	s := &scorer{skills: map[string]float64{}, grades: map[string]bool{}}
	if err := tx.QueryRow(ctx, `SELECT scoring FROM test_types WHERE id=$1`, typeID).Scan(&s.scoring); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT skill_id::text, max_score::float8 FROM test_type_skills WHERE test_type_id=$1`, typeID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		var max float64
		rows.Scan(&id, &max)
		s.skills[id] = max
	}
	rows.Close()
	rows, err = tx.Query(ctx, `SELECT id::text, min_percent::float8 FROM test_grades WHERE archived_at IS NULL ORDER BY min_percent DESC NULLS LAST`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var min *float64
		rows.Scan(&id, &min)
		s.grades[id] = true
		if min != nil {
			s.auto = append(s.auto, autoGrade{id, *min})
		}
	}
	return s, rows.Err()
}

// compute проверяет и считает результат. filled = 0 — ничего не введено; field/code — ошибка ввода.
func (s *scorer) compute(scores []ScoreIn, gradeID *string) (c computed, filled int, field, code string) {
	if gradeID != nil && *gradeID == "" {
		gradeID = nil
	}
	if gradeID != nil && !s.grades[*gradeID] {
		return c, 0, "grade_id", "INVALID_OPTION"
	}
	var total, max float64
	for _, in := range scores {
		sm, ok := s.skills[in.SkillID]
		if !ok {
			return c, 0, "scores", "INVALID_OPTION"
		}
		if s.scoring == "score" {
			if in.Score == nil {
				continue
			}
			if *in.Score < 0 || *in.Score > sm {
				return c, 0, "scores." + in.SkillID, "TOO_LARGE"
			}
			sc, m := round2(*in.Score), sm
			total += sc
			max += sm
			c.rows = append(c.rows, scoreRow{in.SkillID, &sc, &m, nil})
		} else {
			if in.GradeID == nil || *in.GradeID == "" {
				continue
			}
			if !s.grades[*in.GradeID] {
				return c, 0, "scores." + in.SkillID, "INVALID_OPTION"
			}
			g := *in.GradeID
			c.rows = append(c.rows, scoreRow{in.SkillID, nil, nil, &g})
		}
		filled++
	}
	c.gradeID = gradeID
	if s.scoring == "score" && max > 0 {
		t, m, pc := round2(total), max, round2(total/max*100)
		c.total, c.max, c.percent = &t, &m, &pc
		if c.gradeID == nil {
			for _, g := range s.auto {
				if pc >= g.min {
					id := g.id
					c.gradeID = &id
					break
				}
			}
		}
	}
	if gradeID != nil {
		filled++
	}
	return c, filled, "", ""
}

func insertResult(ctx context.Context, tx pgx.Tx, studentID, typeID string, groupID, lessonID, teacherID *string, takenOn string, c computed, comment *string, userID string) (string, error) {
	var id string
	if err := tx.QueryRow(ctx, `INSERT INTO student_tests (tenant_id, student_id, test_type_id, group_id, lesson_id, teacher_id, taken_on, total_score, max_score, percent, grade_id, comment, created_by)
		VALUES (app_current_tenant(), $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12) RETURNING id`,
		studentID, typeID, groupID, lessonID, teacherID, takenOn, c.total, c.max, c.percent, c.gradeID, comment, userID).Scan(&id); err != nil {
		return "", err
	}
	for _, s := range c.rows {
		if _, err := tx.Exec(ctx, `INSERT INTO student_test_scores (tenant_id, test_id, skill_id, score, max_score, grade_id) VALUES (app_current_tenant(), $1, $2, $3, $4, $5)`,
			id, s.skill, s.score, s.maxSc, s.grade); err != nil {
			return "", err
		}
	}
	return id, nil
}

func trimPtr(s *string) *string {
	if s == nil || strings.TrimSpace(*s) == "" {
		return nil
	}
	t := strings.TrimSpace(*s)
	return &t
}

// create — ввод результата вне урока (карточка ученика). Результаты за урок сохраняются вместе с отметкой посещаемости.
func (h *Handler) create(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		TestTypeID string   `json:"test_type_id"`
		GroupID    *string  `json:"group_id"`
		TakenOn    string   `json:"taken_on"`
		Items      []itemIn `json:"items"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	p := auth.FromContext(r.Context())
	if !isStaff(p) {
		return httpx.NewError(403, "USE_LESSON_MARKING", "Результаты тестов вносятся при отметке посещаемости занятия")
	}
	v := &httpx.Validation{}
	if req.TestTypeID == "" {
		v.Add("test_type_id", "REQUIRED")
	}
	taken, err := time.Parse("2006-01-02", req.TakenOn)
	if err != nil {
		v.Add("taken_on", "INVALID_DATE")
	} else if taken.After(time.Now().Add(24 * time.Hour)) {
		v.Add("taken_on", "IN_FUTURE")
	}
	if len(req.Items) == 0 {
		v.Add("items", "REQUIRED")
	} else if len(req.Items) > 200 {
		v.Add("items", "TOO_LARGE")
	}
	if err := v.Err(); err != nil {
		return err
	}
	if req.GroupID != nil && *req.GroupID == "" {
		req.GroupID = nil
	}
	ctx := r.Context()
	ids := []string{}
	err = h.DB.Tx(ctx, func(tx pgx.Tx) error {
		var archived bool
		if err := tx.QueryRow(ctx, `SELECT archived_at IS NOT NULL FROM test_types WHERE id=$1`, req.TestTypeID).Scan(&archived); err != nil {
			if err == pgx.ErrNoRows {
				return (&httpx.Validation{Fields: []httpx.FieldError{{Field: "test_type_id", Code: "INVALID_OPTION"}}}).Err()
			}
			return err
		}
		if archived {
			return httpx.BadRequest("TEST_TYPE_ARCHIVED", "Тип теста в архиве")
		}
		var teacherID *string
		if req.GroupID != nil {
			var branchID string
			if err := tx.QueryRow(ctx, `SELECT branch_id::text, teacher_id::text FROM groups WHERE id=$1`, *req.GroupID).Scan(&branchID, &teacherID); err != nil {
				return err
			}
			if !p.CanBranch(branchID) {
				return httpx.Forbidden()
			}
		}
		if teacherID == nil && p.EmployeeID != "" {
			teacherID = &p.EmployeeID
		}
		sc, err := loadScorer(ctx, tx, req.TestTypeID)
		if err != nil {
			return err
		}
		for i, it := range req.Items {
			fe := func(field, code string) error {
				return (&httpx.Validation{Fields: []httpx.FieldError{{Field: "items." + strconv.Itoa(i) + "." + field, Code: code}}}).Err()
			}
			var branchID string
			if err := tx.QueryRow(ctx, `SELECT branch_id::text FROM students WHERE id=$1 AND archived_at IS NULL`, it.StudentID).Scan(&branchID); err != nil {
				return fe("student_id", "INVALID_OPTION")
			}
			if !p.CanBranch(branchID) {
				return httpx.Forbidden()
			}
			c, filled, field, code := sc.compute(it.Scores, it.GradeID)
			if code != "" {
				return fe(field, code)
			}
			if filled == 0 {
				return fe("scores", "REQUIRED")
			}
			id, err := insertResult(ctx, tx, it.StudentID, req.TestTypeID, req.GroupID, nil, teacherID, req.TakenOn, c, trimPtr(it.Comment), p.UserID)
			if err != nil {
				return err
			}
			if err := database.Audit(ctx, tx, "student", it.StudentID, "test_added", nil, map[string]any{"test_id": id, "test_type_id": req.TestTypeID, "percent": c.percent}); err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return httpx.Created(w, map[string]any{"ids": ids})
}

// ---------------------------------------------------------------- тесты на уроке

// LessonTest — тест, который вводится при отметке занятия, с уже сохранёнными баллами учеников.
type LessonTest struct {
	Type
	Required bool `json:"required"`
}

// LessonEntry — баллы ученика за урок: student_id → test_type_id → skill_id → балл или оценка.
type LessonEntry struct {
	StudentID  string    `json:"student_id"`
	TestTypeID string    `json:"test_type_id"`
	Scores     []ScoreIn `json:"scores"`
}

// ForLesson возвращает тесты урока (по курсу группы), шкалу оценок и введённые баллы.
func ForLesson(ctx context.Context, tx pgx.Tx, lessonID, courseID string) (tests []LessonTest, grades []Grade, entries []LessonEntry, err error) {
	cfg, err := LoadConfig(ctx, tx, false, true)
	if err != nil {
		return nil, nil, nil, err
	}
	tests = []LessonTest{}
	for _, t := range cfg.Types {
		if len(t.CourseIDs) == 0 || slices.Contains(t.CourseIDs, courseID) {
			tests = append(tests, LessonTest{Type: t, Required: t.LessonMode == "required"})
		}
	}
	entries = []LessonEntry{}
	rows, err := tx.Query(ctx, `SELECT st.student_id::text, st.test_type_id::text, sc.skill_id::text, sc.score::float8, sc.grade_id::text
		FROM student_tests st JOIN student_test_scores sc ON sc.test_id=st.id WHERE st.lesson_id=$1 ORDER BY 1, 2`, lessonID)
	if err != nil {
		return nil, nil, nil, err
	}
	defer rows.Close()
	idx := map[string]int{}
	for rows.Next() {
		var sid, tid string
		var s ScoreIn
		if err := rows.Scan(&sid, &tid, &s.SkillID, &s.Score, &s.GradeID); err != nil {
			return nil, nil, nil, err
		}
		k := sid + "|" + tid
		i, ok := idx[k]
		if !ok {
			i = len(entries)
			idx[k] = i
			entries = append(entries, LessonEntry{StudentID: sid, TestTypeID: tid})
		}
		entries[i].Scores = append(entries[i].Scores, s)
	}
	return tests, cfg.Grades, entries, rows.Err()
}

// SaveLesson заменяет результаты тестов урока. present — ученики, пришедшие на урок; при закрытии
// по ним проверяется заполнение обязательных тестов. Возвращает id учеников с результатами.
func SaveLesson(ctx context.Context, tx pgx.Tx, lessonID, groupID, courseID, takenOn string, teacherID *string, userID string,
	entries []LessonEntry, present map[string]bool, enforceRequired bool) error {
	tests, _, _, err := ForLesson(ctx, tx, lessonID, courseID)
	if err != nil {
		return err
	}
	allowed := map[string]LessonTest{}
	for _, t := range tests {
		allowed[t.ID] = t
	}
	scorers := map[string]*scorer{}
	filledBy := map[string]bool{} // student|type
	type result struct {
		e LessonEntry
		c computed
	}
	var results []result
	for i, e := range entries {
		if _, ok := allowed[e.TestTypeID]; !ok {
			return (&httpx.Validation{Fields: []httpx.FieldError{{Field: "tests." + strconv.Itoa(i) + ".test_type_id", Code: "INVALID_OPTION"}}}).Err()
		}
		if !present[e.StudentID] {
			continue // отсутствующим баллы не ставятся
		}
		sc := scorers[e.TestTypeID]
		if sc == nil {
			if sc, err = loadScorer(ctx, tx, e.TestTypeID); err != nil {
				return err
			}
			scorers[e.TestTypeID] = sc
		}
		c, filled, field, code := sc.compute(e.Scores, nil)
		if code != "" {
			return (&httpx.Validation{Fields: []httpx.FieldError{{Field: "tests." + strconv.Itoa(i) + "." + field, Code: code}}}).Err()
		}
		if filled == 0 {
			continue
		}
		filledBy[e.StudentID+"|"+e.TestTypeID] = true
		results = append(results, result{e, c})
	}
	if enforceRequired {
		missing := 0
		for sid := range present {
			for _, t := range tests {
				if t.Required && !filledBy[sid+"|"+t.ID] {
					missing++
				}
			}
		}
		if missing > 0 {
			return httpx.NewError(422, "LESSON_TESTS_REQUIRED", "Заполните обязательные тесты у всех пришедших учеников (не заполнено: "+strconv.Itoa(missing)+")")
		}
	}
	// результаты урока пересохраняются целиком: так снятый балл удаляется, а исправленный — заменяется
	if _, err := tx.Exec(ctx, `DELETE FROM student_tests WHERE lesson_id=$1`, lessonID); err != nil {
		return err
	}
	for _, r := range results {
		gid, lid := groupID, lessonID
		if _, err := insertResult(ctx, tx, r.e.StudentID, r.e.TestTypeID, &gid, &lid, teacherID, takenOn, r.c, nil, userID); err != nil {
			return err
		}
	}
	return nil
}

// remove: администратор — любой результат своего филиала; преподаватель — только внесённый им самим.
func (h *Handler) remove(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	p := auth.FromContext(r.Context())
	ctx := r.Context()
	err := h.DB.Tx(ctx, func(tx pgx.Tx) error {
		var studentID, branchID string
		var createdBy *string
		if err := tx.QueryRow(ctx, `SELECT st.student_id::text, s.branch_id::text, st.created_by::text FROM student_tests st JOIN students s ON s.id=st.student_id WHERE st.id=$1`, id).
			Scan(&studentID, &branchID, &createdBy); err != nil {
			return err
		}
		own := createdBy != nil && *createdBy == p.UserID
		if !own && !(p.Can("students.edit") && p.CanBranch(branchID)) {
			return httpx.Forbidden()
		}
		if _, err := tx.Exec(ctx, `DELETE FROM student_tests WHERE id=$1`, id); err != nil {
			return err
		}
		return database.Audit(ctx, tx, "student", studentID, "test_deleted", map[string]any{"test_id": id}, nil)
	})
	if err != nil {
		return err
	}
	return httpx.NoContent(w)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) error {
	p := auth.FromContext(r.Context())
	q := r.URL.Query()
	limit := httpx.QueryInt(r, "limit", 50, 1000)
	export := q.Get("format") == "xlsx"
	if export {
		limit = 20000
	}
	ctx := r.Context()
	var out []Result
	err := h.DB.Tx(ctx, func(tx pgx.Tx) error {
		where := []string{"true"}
		args := []any{}
		add := func(cond string, v any) {
			args = append(args, v)
			where = append(where, strings.ReplaceAll(cond, "$?", "$"+strconv.Itoa(len(args))))
		}
		if sid := q.Get("student_id"); sid != "" {
			add("st.student_id = $?", sid)
		}
		if gid := q.Get("group_id"); gid != "" {
			add("st.group_id = $?", gid)
		}
		if tid := q.Get("test_type_id"); tid != "" {
			add("st.test_type_id = $?", tid)
		}
		if v := q.Get("teacher_id"); v != "" {
			add("st.teacher_id = $?", v)
		}
		if v := q.Get("from"); v != "" {
			add("st.taken_on >= $?::date", v)
		}
		if v := q.Get("to"); v != "" {
			add("st.taken_on <= $?::date", v)
		}
		if v := strings.TrimSpace(q.Get("q")); v != "" {
			add("s.full_name ILIKE '%' || $? || '%'", v)
		}
		if v := q.Get("source"); v == "lesson" {
			where = append(where, "st.lesson_id IS NOT NULL")
		} else if v == "manual" {
			where = append(where, "st.lesson_id IS NULL")
		}
		if isStaff(p) {
			if b := p.BranchFilter(); b != nil {
				add("s.branch_id = ANY($?::uuid[])", b)
			}
		} else {
			// преподаватель: результаты своих групп и внесённые им самим
			args = append(args, p.EmployeeID, p.UserID)
			n := len(args)
			where = append(where, "(g.teacher_id::text = $"+strconv.Itoa(n-1)+" OR st.created_by::text = $"+strconv.Itoa(n)+")")
		}
		var err error
		out, err = LoadResults(ctx, tx, strings.Join(where, " AND "), args, limit)
		return err
	})
	if err != nil {
		return err
	}
	if export {
		return exportXLSX(w, out)
	}
	return httpx.OK(w, out)
}

// exportXLSX — выгрузка результатов: строка на результат, баллы по навыкам — отдельными столбцами.
func exportXLSX(w http.ResponseWriter, list []Result) error {
	x := excelize.NewFile()
	defer x.Close()
	sheet := "Результаты тестов"
	x.SetSheetName("Sheet1", sheet)
	skills := []string{}
	seen := map[string]bool{}
	for _, r := range list {
		for _, s := range r.Scores {
			if !seen[s.Skill] {
				seen[s.Skill] = true
				skills = append(skills, s.Skill)
			}
		}
	}
	head := []any{"Дата", "Ученик", "Тест", "Группа", "Преподаватель", "Источник", "Баллы", "Максимум", "%", "Оценка"}
	for _, s := range skills {
		head = append(head, s)
	}
	head = append(head, "Комментарий")
	put := func(row int, vals []any) {
		for j, v := range vals {
			cell, _ := excelize.CoordinatesToCellName(j+1, row)
			x.SetCellValue(sheet, cell, v)
		}
	}
	put(1, head)
	bold, _ := x.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	last, _ := excelize.CoordinatesToCellName(len(head), 1)
	x.SetCellStyle(sheet, "A1", last, bold)
	opt := func(v *float64) any {
		if v == nil {
			return ""
		}
		return *v
	}
	str := func(v *string) string {
		if v == nil {
			return ""
		}
		return *v
	}
	for i, r := range list {
		d, _ := time.Parse("2006-01-02", r.TakenOn)
		src := "Карточка ученика"
		if r.LessonID != nil {
			src = "Урок"
		}
		grade := ""
		if r.Grade != nil {
			grade = r.Grade.Name
		}
		row := []any{d.Format("02.01.2006"), r.Student, r.TestType, str(r.Group), str(r.Teacher), src, opt(r.TotalScore), opt(r.MaxScore), opt(r.Percent), grade}
		by := map[string]string{}
		for _, s := range r.Scores {
			switch {
			case s.Grade != nil:
				by[s.Skill] = s.Grade.Name
			case s.Score != nil && s.MaxScore != nil:
				by[s.Skill] = strconv.FormatFloat(*s.Score, 'f', -1, 64) + "/" + strconv.FormatFloat(*s.MaxScore, 'f', -1, 64)
			}
		}
		for _, s := range skills {
			row = append(row, by[s])
		}
		row = append(row, str(r.Comment))
		put(i+2, row)
	}
	x.SetColWidth(sheet, "B", "E", 24)
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="tests_`+time.Now().Format("2006-01-02")+`.xlsx"`)
	return x.Write(w)
}

type ScoreOut struct {
	SkillID  string    `json:"skill_id"`
	Skill    string    `json:"skill"`
	Score    *float64  `json:"score"`
	MaxScore *float64  `json:"max_score"`
	Grade    *GradeRef `json:"grade"`
}

type GradeRef struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	Color *string `json:"color"`
}

type Result struct {
	ID         string     `json:"id"`
	TakenOn    string     `json:"taken_on"`
	StudentID  string     `json:"student_id"`
	Student    string     `json:"student"`
	TestTypeID string     `json:"test_type_id"`
	TestType   string     `json:"test_type"`
	Scoring    string     `json:"scoring"`
	Group      *string    `json:"group"`
	LessonID   *string    `json:"lesson_id"`
	Teacher    *string    `json:"teacher"`
	TotalScore *float64   `json:"total_score"`
	MaxScore   *float64   `json:"max_score"`
	Percent    *float64   `json:"percent"`
	Grade      *GradeRef  `json:"grade"`
	Comment    *string    `json:"comment"`
	CreatedBy  *string    `json:"created_by"`
	CreatedAt  time.Time  `json:"created_at"`
	Scores     []ScoreOut `json:"scores"`
}

// LoadResults — результаты тестов с баллами по навыкам. where использует алиасы st (student_tests), s (students), g (groups).
func LoadResults(ctx context.Context, tx pgx.Tx, where string, args []any, limit int) ([]Result, error) {
	out := []Result{}
	rows, err := tx.Query(ctx, `SELECT st.id::text, to_char(st.taken_on, 'YYYY-MM-DD'), s.id::text, s.full_name, t.id::text, t.name, t.scoring, g.name, st.lesson_id::text, e.full_name,
			st.total_score::float8, st.max_score::float8, st.percent::float8, gr.id::text, gr.name, gr.color, st.comment, st.created_by::text, st.created_at
		FROM student_tests st JOIN students s ON s.id=st.student_id JOIN test_types t ON t.id=st.test_type_id
		LEFT JOIN groups g ON g.id=st.group_id LEFT JOIN employees e ON e.id=st.teacher_id LEFT JOIN test_grades gr ON gr.id=st.grade_id
		WHERE `+where+` ORDER BY st.taken_on DESC, st.created_at DESC LIMIT `+strconv.Itoa(limit), args...)
	if err != nil {
		return nil, err
	}
	idx := map[string]int{}
	ids := []string{}
	for rows.Next() {
		res := Result{Scores: []ScoreOut{}}
		var gID, gName, gColor *string
		if err := rows.Scan(&res.ID, &res.TakenOn, &res.StudentID, &res.Student, &res.TestTypeID, &res.TestType, &res.Scoring, &res.Group, &res.LessonID, &res.Teacher,
			&res.TotalScore, &res.MaxScore, &res.Percent, &gID, &gName, &gColor, &res.Comment, &res.CreatedBy, &res.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		if gID != nil {
			res.Grade = &GradeRef{*gID, *gName, gColor}
		}
		idx[res.ID] = len(out)
		ids = append(ids, res.ID)
		out = append(out, res)
	}
	rows.Close()
	if err := rows.Err(); err != nil || len(ids) == 0 {
		return out, err
	}
	rows, err = tx.Query(ctx, `SELECT sc.test_id::text, sc.skill_id::text, sk.name, sc.score::float8, sc.max_score::float8, gr.id::text, gr.name, gr.color
		FROM student_test_scores sc JOIN test_skills sk ON sk.id=sc.skill_id LEFT JOIN test_grades gr ON gr.id=sc.grade_id
		JOIN student_tests st ON st.id=sc.test_id
		LEFT JOIN test_type_skills ts ON ts.test_type_id=st.test_type_id AND ts.skill_id=sc.skill_id
		WHERE sc.test_id = ANY($1::uuid[]) ORDER BY ts.sort_order NULLS LAST, sk.name`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var testID string
		var s ScoreOut
		var gID, gName, gColor *string
		if err := rows.Scan(&testID, &s.SkillID, &s.Skill, &s.Score, &s.MaxScore, &gID, &gName, &gColor); err != nil {
			return nil, err
		}
		if gID != nil {
			s.Grade = &GradeRef{*gID, *gName, gColor}
		}
		out[idx[testID]].Scores = append(out[idx[testID]].Scores, s)
	}
	return out, rows.Err()
}
