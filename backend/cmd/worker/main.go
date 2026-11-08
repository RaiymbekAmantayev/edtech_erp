package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"emp/internal/modules/notification"
	"emp/internal/platform/config"
	"emp/internal/platform/database"
)

// Worker: outbox → уведомления, Telegram-бот, продление горизонта расписания, статусы абонементов.
// Работает под системной ролью emp_owner (доступ ко всем tenant'ам), запуск логируется.
func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	db, err := database.Connect(ctx, cfg.DatabaseURL, cfg.SystemURL)
	if err != nil {
		slog.Error("database", "err", err)
		os.Exit(1)
	}
	defer db.Close()
	(&notification.Worker{DB: db, BotToken: cfg.TelegramBotToken, PublicURL: cfg.PublicURL}).Run(ctx)
	slog.Info("worker stopped")
}
