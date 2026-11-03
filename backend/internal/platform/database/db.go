package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"

	"emp/internal/platform/auth"
	"emp/internal/platform/httpx"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DB объединяет два пула:
//   - App: роль emp_app, все бизнес-запросы под RLS;
//   - Sys: роль emp_owner, только аутентификация, worker и агрегаты платформы.
type DB struct {
	App *pgxpool.Pool
	Sys *pgxpool.Pool
}

func Connect(ctx context.Context, appURL, sysURL string) (*DB, error) {
	app, err := pgxpool.New(ctx, appURL)
	if err != nil {
		return nil, err
	}
	sys, err := pgxpool.New(ctx, sysURL)
	if err != nil {
		return nil, err
	}
	if err := app.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping app pool: %w", err)
	}
	return &DB{App: app, Sys: sys}, nil
}

func (d *DB) Close() { d.App.Close(); d.Sys.Close() }

// Tx выполняет fn в транзакции с tenant-контекстом текущего пользователя.
// set_config(..., true) действует только до конца транзакции и не «протекает» в соседние запросы пула.
func (d *DB) Tx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	p := auth.FromContext(ctx)
	if p == nil || p.TenantID == "" {
		return httpx.Unauthorized()
	}
	return d.InTenant(ctx, p.TenantID, fn)
}

func (d *DB) InTenant(ctx context.Context, tenantID string, fn func(tx pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, d.App, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
			return err
		}
		return fn(tx)
	})
}

// ---------------------------------------------------------------- аудит и outbox

// Audit пишет запись в audit_logs в текущей транзакции.
func Audit(ctx context.Context, tx pgx.Tx, entity, entityID, action string, oldV, newV any) error {
	p := auth.FromContext(ctx)
	var uid, ip, ua *string
	if p != nil {
		uid, ip, ua = &p.UserID, &p.IP, &p.UserAgent
	}
	_, err := tx.Exec(ctx, `INSERT INTO audit_logs (tenant_id, user_id, entity_type, entity_id, action, old_values, new_values, ip_address, user_agent)
		VALUES (app_current_tenant(), $1, $2, NULLIF($3,'')::uuid, $4, $5, $6, $7, $8)`,
		uid, entity, entityID, action, jsonOrNil(oldV), jsonOrNil(newV), ip, ua)
	return err
}

func jsonOrNil(v any) []byte {
	if v == nil {
		return nil
	}
	b, _ := json.Marshal(v)
	return b
}

// Publish записывает доменное событие в outbox в той же транзакции, что и бизнес-изменение.
func Publish(ctx context.Context, tx pgx.Tx, eventType string, payload any) error {
	b, _ := json.Marshal(payload)
	_, err := tx.Exec(ctx, `INSERT INTO outbox_events (tenant_id, event_type, payload) VALUES (app_current_tenant(), $1, $2)`, eventType, b)
	return err
}

// ---------------------------------------------------------------- миграции

func Migrate(ctx context.Context, pool *pgxpool.Pool, files fs.FS) error {
	if _, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`); err != nil {
		return err
	}
	names, err := fs.Glob(files, "*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	for _, name := range names {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version=$1)`, name).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		sqlBytes, err := fs.ReadFile(files, name)
		if err != nil {
			return err
		}
		err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, string(sqlBytes)); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			_, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, name)
			return err
		})
		if err != nil {
			return err
		}
		slog.Info("migration applied", "file", name)
	}
	return nil
}

// Setup создаёт роли и базу данных от имени суперпользователя (идемпотентно).
func Setup(ctx context.Context, adminURL, appURL, sysURL string) error {
	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		return fmt.Errorf("connect as admin: %w", err)
	}
	defer admin.Close(ctx)

	appCfg, err := pgx.ParseConfig(appURL)
	if err != nil {
		return err
	}
	sysCfg, err := pgx.ParseConfig(sysURL)
	if err != nil {
		return err
	}
	ensureRole := func(name, pw string) error {
		var exists bool
		admin.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname=$1)`, name).Scan(&exists)
		q := "CREATE ROLE %s LOGIN NOSUPERUSER NOBYPASSRLS PASSWORD %s"
		if exists {
			q = "ALTER ROLE %s LOGIN NOSUPERUSER NOBYPASSRLS PASSWORD %s"
		}
		_, err := admin.Exec(ctx, fmt.Sprintf(q, pgx.Identifier{name}.Sanitize(), quoteLiteral(pw)))
		return err
	}
	if err := ensureRole(sysCfg.User, sysCfg.Password); err != nil {
		return err
	}
	if err := ensureRole(appCfg.User, appCfg.Password); err != nil {
		return err
	}
	var dbExists bool
	admin.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname=$1)`, sysCfg.Database).Scan(&dbExists)
	if !dbExists {
		if _, err := admin.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s OWNER %s ENCODING 'UTF8' TEMPLATE template0",
			pgx.Identifier{sysCfg.Database}.Sanitize(), pgx.Identifier{sysCfg.User}.Sanitize())); err != nil {
			return err
		}
		slog.Info("database created", "name", sysCfg.Database)
	}
	// В PG15+ схема public принадлежит pg_database_owner — владелец БД может создавать объекты.
	return nil
}

// Drop удаляет базу данных (только для локальной разработки: migrate -reset).
func Drop(ctx context.Context, adminURL, sysURL string) error {
	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		return err
	}
	defer admin.Close(ctx)
	cfg, err := pgx.ParseConfig(sysURL)
	if err != nil {
		return err
	}
	_, err = admin.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", pgx.Identifier{cfg.Database}.Sanitize()))
	return err
}

func quoteLiteral(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

var ErrNoTenant = errors.New("no tenant")
