package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"emp/internal/modules/attendance"
	"emp/internal/modules/branch"
	"emp/internal/modules/customfield"
	"emp/internal/modules/employee"
	"emp/internal/modules/exam"
	"emp/internal/modules/finance"
	"emp/internal/modules/group"
	"emp/internal/modules/identity"
	"emp/internal/modules/lead"
	"emp/internal/modules/parent"
	"emp/internal/modules/platform"
	"emp/internal/modules/portal"
	"emp/internal/modules/reporting"
	"emp/internal/modules/schedule"
	"emp/internal/modules/student"
	"emp/internal/platform/auth"
	"emp/internal/platform/config"
	"emp/internal/platform/database"
	"emp/internal/platform/httpx"
	mw "emp/internal/platform/middleware"
	"emp/internal/platform/telegram"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
)

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

	r := chi.NewRouter()
	r.Use(chimw.RequestID, chimw.RealIP, mw.Logger, chimw.Recoverer)
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Referrer-Policy", "same-origin")
			next.ServeHTTP(w, r)
		})
	})

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		if err := db.App.Ping(r.Context()); err != nil {
			http.Error(w, "db down", http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte("ok"))
	})

	authn := &mw.Authenticator{DB: db, Secret: cfg.JWTSecret}
	idh := &identity.Handler{DB: db, Secret: cfg.JWTSecret, CookieSecure: cfg.CookieSecure}
	bots := telegram.Platform{Token: cfg.TelegramBotToken, Username: cfg.TelegramBotName}
	cabinet := &portal.Handler{DB: db, Secret: cfg.JWTSecret, Platform: bots, PublicURL: cfg.PublicURL, Staff: idh}

	r.Route("/api/v1", func(r chi.Router) {
		idh.PublicRoutes(r)
		// личный кабинет (Telegram Mini App): вход по ИИН или через Telegram
		cabinet.PublicRoutes(r)
		// приём заявок с сайта: tenant определяется по публичному ключу формы
		r.With(mw.RateLimit(30, time.Minute)).Post("/public/{tenantID}/leads", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			return lead.PublicForm(w, r, db)
		}).ServeHTTP)

		r.Group(func(r chi.Router) {
			r.Use(authn.Require)
			idh.PrivateRoutes(r)
			r.Group(func(r chi.Router) {
				r.Use(mw.PlatformAdmin)
				(&platform.Handler{DB: db}).Routes(r)
			})
			r.Group(func(r chi.Router) {
				r.Use(mw.TenantUser)
				(&branch.Handler{DB: db}).Routes(r)
				(&customfield.Handler{DB: db}).Routes(r)
				(&student.Handler{DB: db, BotName: cfg.TelegramBotName}).Routes(r)
				(&parent.Handler{DB: db, BotName: cfg.TelegramBotName}).Routes(r)
				(&employee.Handler{DB: db}).Routes(r)
				(&group.Handler{DB: db}).Routes(r)
				(&schedule.Handler{DB: db}).Routes(r)
				(&attendance.Handler{DB: db}).Routes(r)
				(&finance.Handler{DB: db}).Routes(r)
				(&lead.Handler{DB: db}).Routes(r)
				(&reporting.Handler{DB: db}).Routes(r)
				(&exam.Handler{DB: db}).Routes(r)
				cabinet.StaffRoutes(r)
			})
		})
	})

	srv := &http.Server{Addr: cfg.HTTPAddr, Handler: r, ReadHeaderTimeout: 10 * time.Second, WriteTimeout: 60 * time.Second}
	go func() {
		slog.Info("api listening", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server", "err", err)
			stop()
		}
	}()
	<-ctx.Done()
	// корректное завершение: дожидаемся текущих запросов
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	srv.Shutdown(shutdownCtx)
	slog.Info("api stopped")
	_ = auth.AccessTTL
}
