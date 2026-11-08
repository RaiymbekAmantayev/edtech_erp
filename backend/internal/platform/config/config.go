package config

import (
	"bufio"
	"os"
	"strings"
)

type Config struct {
	HTTPAddr         string
	DatabaseURL      string // роль emp_app — под RLS
	SystemURL        string // роль emp_owner — миграции, worker, super admin агрегаты
	AdminURL         string // суперпользователь postgres — только для первичной настройки
	JWTSecret        string
	TelegramBotToken string
	TelegramBotName  string
	CookieSecure     bool
	PublicURL        string // адрес веб-интерфейса — для ссылок на Mini App
}

// Load читает .env (если есть) и переменные окружения. Переменные окружения имеют приоритет.
func Load() Config {
	loadDotEnv(".env")
	return Config{
		HTTPAddr:         get("HTTP_ADDR", ":8080"),
		DatabaseURL:      get("DATABASE_URL", "postgres://emp_app:emp_app_dev@localhost:5432/emp?sslmode=disable"),
		SystemURL:        get("DATABASE_SYSTEM_URL", "postgres://emp_owner:emp_owner_dev@localhost:5432/emp?sslmode=disable"),
		AdminURL:         get("POSTGRES_ADMIN_URL", "postgres://postgres:rrr@localhost:5432/postgres?sslmode=disable"),
		JWTSecret:        get("JWT_SECRET", "dev-secret-change-me-please-32bytes!!"),
		TelegramBotToken: get("TELEGRAM_BOT_TOKEN", ""),
		TelegramBotName:  get("TELEGRAM_BOT_NAME", "emp_center_bot"),
		CookieSecure:     get("COOKIE_SECURE", "false") == "true",
		PublicURL:        get("PUBLIC_URL", "http://localhost:5173"),
	}
}

func get(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		if _, exists := os.LookupEnv(k); !exists {
			os.Setenv(k, strings.Trim(strings.TrimSpace(v), `"`))
		}
	}
}
