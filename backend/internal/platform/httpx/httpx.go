package httpx

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Error — бизнес-ошибка с кодом из стабильного списка (ТЗ раздел 30).
type Error struct {
	Status  int          `json:"-"`
	Code    string       `json:"code"`
	Message string       `json:"message"`
	Fields  []FieldError `json:"fields,omitempty"`
}

type FieldError struct {
	Field string `json:"field"`
	Code  string `json:"code"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func NewError(status int, code, msg string) *Error { return &Error{Status: status, Code: code, Message: msg} }

func BadRequest(code, msg string) *Error { return NewError(http.StatusBadRequest, code, msg) }
func NotFound(code string) *Error        { return NewError(http.StatusNotFound, code, "Не найдено") }
func Forbidden() *Error                  { return NewError(http.StatusForbidden, "FORBIDDEN", "Недостаточно прав") }
func Unauthorized() *Error {
	return NewError(http.StatusUnauthorized, "UNAUTHORIZED", "Требуется авторизация")
}
func Conflict(code, msg string) *Error { return NewError(http.StatusConflict, code, msg) }

// Validation собирает ошибки полей.
type Validation struct{ Fields []FieldError }

func (v *Validation) Add(field, code string) { v.Fields = append(v.Fields, FieldError{field, code}) }
func (v *Validation) Err() error {
	if len(v.Fields) == 0 {
		return nil
	}
	return &Error{Status: http.StatusUnprocessableEntity, Code: "VALIDATION_FAILED", Message: "Проверьте заполнение полей", Fields: v.Fields}
}

// Handler — обработчик, возвращающий ошибку.
type Handler func(w http.ResponseWriter, r *http.Request) error

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := h(w, r); err != nil {
		WriteError(w, r, err)
	}
}

func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	var e *Error
	if errors.As(err, &e) {
		writeJSON(w, e.Status, map[string]any{"error": e})
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": NotFound("NOT_FOUND")})
		return
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			writeJSON(w, http.StatusConflict, map[string]any{"error": Conflict("DUPLICATE", "Запись с такими данными уже существует")})
			return
		case "23503":
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": BadRequest("INVALID_REFERENCE", "Связанная запись не найдена")})
			return
		case "22P02":
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": BadRequest("INVALID_FORMAT", "Неверный формат значения")})
			return
		}
	}
	slog.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	writeJSON(w, http.StatusInternalServerError, map[string]any{"error": NewError(500, "INTERNAL", "Внутренняя ошибка сервера")})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// OK пишет {"data": ..., "meta": ...}.
func OK(w http.ResponseWriter, data any) error {
	writeJSON(w, http.StatusOK, map[string]any{"data": data, "meta": map[string]any{}})
	return nil
}

func OKMeta(w http.ResponseWriter, data any, meta map[string]any) error {
	writeJSON(w, http.StatusOK, map[string]any{"data": data, "meta": meta})
	return nil
}

func Created(w http.ResponseWriter, data any) error {
	writeJSON(w, http.StatusCreated, map[string]any{"data": data, "meta": map[string]any{}})
	return nil
}

func NoContent(w http.ResponseWriter) error {
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func Decode(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 5<<20))
	if err := dec.Decode(v); err != nil {
		return BadRequest("INVALID_JSON", "Некорректное тело запроса")
	}
	return nil
}

func QueryInt(r *http.Request, key string, def, max int) int {
	v, err := strconv.Atoi(r.URL.Query().Get(key))
	if err != nil || v <= 0 {
		return def
	}
	if max > 0 && v > max {
		return max
	}
	return v
}

func Trim(s string) string { return strings.TrimSpace(s) }

// Ptr возвращает nil для пустой строки — удобно для nullable колонок.
func Ptr(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}
