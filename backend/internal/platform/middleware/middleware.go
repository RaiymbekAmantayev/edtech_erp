package middleware

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"emp/internal/platform/auth"
	"emp/internal/platform/database"
	"emp/internal/platform/httpx"

	"github.com/jackc/pgx/v5"
)

// Logger пишет структурированный лог без персональных данных: метод, путь, статус, длительность, ID пользователя.
func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(sw, r)
		uid := ""
		if p := auth.FromContext(r.Context()); p != nil {
			uid = p.UserID
		}
		slog.Info("http", "method", r.Method, "path", r.URL.Path, "status", sw.status,
			"ms", time.Since(start).Milliseconds(), "user", uid)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) { s.status = code; s.ResponseWriter.WriteHeader(code) }

func ClientIP(r *http.Request) string {
	if xf := r.Header.Get("X-Forwarded-For"); xf != "" {
		return strings.TrimSpace(strings.Split(xf, ",")[0])
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	return host
}

// Authenticator проверяет JWT и загружает права пользователя из БД.
type Authenticator struct {
	DB     *database.DB
	Secret string
}

func (a *Authenticator) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		token, ok := strings.CutPrefix(h, "Bearer ")
		if !ok {
			httpx.WriteError(w, r, httpx.Unauthorized())
			return
		}
		claims, err := auth.ParseAccess(a.Secret, token)
		if err != nil {
			httpx.WriteError(w, r, httpx.NewError(401, "TOKEN_EXPIRED", "Сессия истекла"))
			return
		}
		p := &auth.Principal{UserID: claims.Subject, TenantID: claims.TenantID, IsPlatformAdmin: claims.Platform,
			Permissions: map[string]bool{}, IP: ClientIP(r), UserAgent: r.UserAgent()}
		if p.TenantID != "" {
			if err := a.load(r.Context(), p); err != nil {
				httpx.WriteError(w, r, httpx.Unauthorized())
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(auth.WithPrincipal(r.Context(), p)))
	})
}

func (a *Authenticator) load(ctx context.Context, p *auth.Principal) error {
	return a.DB.InTenant(ctx, p.TenantID, func(tx pgx.Tx) error {
		var status string
		if err := tx.QueryRow(ctx, `SELECT full_name, status FROM users WHERE id=$1`, p.UserID).Scan(&p.FullName, &status); err != nil {
			return err
		}
		if status != "active" {
			return httpx.Unauthorized()
		}
		rows, err := tx.Query(ctx, `SELECT r.code, r.all_branches, r.permissions FROM user_roles ur JOIN roles r ON r.id=ur.role_id WHERE ur.user_id=$1`, p.UserID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var code string
			var all bool
			var perms []string
			if err := rows.Scan(&code, &all, &perms); err != nil {
				return err
			}
			p.RoleCodes = append(p.RoleCodes, code)
			p.AllBranches = p.AllBranches || all
			for _, perm := range perms {
				p.Permissions[perm] = true // итоговые права — объединение ролей
			}
		}
		rows.Close()
		if !p.AllBranches {
			rows, err := tx.Query(ctx, `SELECT branch_id::text FROM user_branch_access WHERE user_id=$1`, p.UserID)
			if err != nil {
				return err
			}
			p.BranchIDs, err = pgx.CollectRows(rows, pgx.RowTo[string])
			if err != nil {
				return err
			}
		}
		tx.QueryRow(ctx, `SELECT id::text FROM employees WHERE user_id=$1 AND status='active' LIMIT 1`, p.UserID).Scan(&p.EmployeeID)
		return nil
	})
}

// Perm требует хотя бы одно из перечисленных прав. Проверка на сервере — скрытие кнопки в UI защитой не является.
func Perm(perms ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := auth.FromContext(r.Context())
			for _, perm := range perms {
				if p.Can(perm) {
					next.ServeHTTP(w, r)
					return
				}
			}
			httpx.WriteError(w, r, httpx.Forbidden())
		})
	}
}

// TenantUser — только пользователи организаций.
func TenantUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p := auth.FromContext(r.Context()); p == nil || p.TenantID == "" {
			httpx.WriteError(w, r, httpx.Forbidden())
			return
		}
		next.ServeHTTP(w, r)
	})
}

func PlatformAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p := auth.FromContext(r.Context()); p == nil || !p.IsPlatformAdmin {
			httpx.WriteError(w, r, httpx.Forbidden())
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RateLimit — простой ограничитель «N запросов в окно» по IP (для логина и публичных форм).
func RateLimit(n int, window time.Duration) func(http.Handler) http.Handler {
	type bucket struct {
		count int
		reset time.Time
	}
	var mu sync.Mutex
	buckets := map[string]*bucket{}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := ClientIP(r)
			mu.Lock()
			b := buckets[ip]
			now := time.Now()
			if b == nil || now.After(b.reset) {
				b = &bucket{reset: now.Add(window)}
				buckets[ip] = b
			}
			b.count++
			over := b.count > n
			mu.Unlock()
			if over {
				httpx.WriteError(w, r, httpx.NewError(429, "RATE_LIMITED", "Слишком много запросов, попробуйте позже"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
