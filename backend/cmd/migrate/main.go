package main

import (
	"context"
	"flag"
	"log/slog"
	"os"

	"emp/internal/platform/config"
	"emp/internal/platform/database"
	"emp/internal/seed"
	"emp/migrations"

	"github.com/jackc/pgx/v5/pgxpool"
)

// migrate: -setup создаёт роли и БД (суперпользователь postgres), затем применяет миграции; -seed загружает демо-данные.
func main() {
	setup := flag.Bool("setup", false, "создать роли emp_owner/emp_app и базу данных")
	doSeed := flag.Bool("seed", false, "загрузить демо-данные (только в пустую базу)")
	reset := flag.Bool("reset", false, "удалить базу и создать заново (только для локальной разработки)")
	flag.Parse()
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))
	cfg := config.Load()
	ctx := context.Background()

	if *reset {
		if err := database.Drop(ctx, cfg.AdminURL, cfg.SystemURL); err != nil {
			slog.Error("reset", "err", err)
			os.Exit(1)
		}
		*setup = true
	}
	if *setup {
		if err := database.Setup(ctx, cfg.AdminURL, cfg.DatabaseURL, cfg.SystemURL); err != nil {
			slog.Error("setup", "err", err)
			os.Exit(1)
		}
	}
	sys, err := pgxpool.New(ctx, cfg.SystemURL)
	if err != nil {
		slog.Error("connect", "err", err)
		os.Exit(1)
	}
	if err := database.Migrate(ctx, sys, migrations.FS); err != nil {
		slog.Error("migrate", "err", err)
		os.Exit(1)
	}
	sys.Close()
	slog.Info("migrations up to date")

	if *doSeed {
		db, err := database.Connect(ctx, cfg.DatabaseURL, cfg.SystemURL)
		if err != nil {
			slog.Error("connect", "err", err)
			os.Exit(1)
		}
		defer db.Close()
		if err := seed.Run(ctx, db); err != nil {
			slog.Error("seed", "err", err)
			os.Exit(1)
		}
	}
}
