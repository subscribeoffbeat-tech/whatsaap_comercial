package main

import (
	"context"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"whatsapptool/internal/campaigns"
	"whatsapptool/internal/config"
	"whatsapptool/internal/db"
	"whatsapptool/internal/web/handlers"
	mw "whatsapptool/internal/web/middleware"
	"whatsapptool/internal/web/ws"
	"whatsapptool/internal/whatsapp"
	"whatsapptool/static"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	ctx := context.Background()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer pool.Close()

	if err := db.RunMigrations(ctx, pool); err != nil {
		log.Fatalf("migrations: %v", err)
	}

	if err := db.EnsureStopRule(ctx, pool); err != nil {
		log.Printf("ensure stop rule: %v", err)
	}

	db.StartPurgeWorker(ctx, pool)

	if err := campaigns.RunRiverMigrations(ctx, pool); err != nil {
		log.Fatalf("river migrations: %v", err)
	}

	// Infrastructure
	webhookStore := db.NewWebhookStore(pool)
	proc         := whatsapp.NewProcessor(webhookStore)
	hub          := ws.NewHub()
	waClient     := whatsapp.NewClient(cfg.WA.PhoneNumberID, cfg.WA.WABAID, cfg.WA.AccessToken)

	// River queue
	riverClient, err := campaigns.NewRiverClient(pool, waClient, hub)
	if err != nil {
		log.Fatalf("river client: %v", err)
	}
	if err := riverClient.Start(ctx); err != nil {
		log.Fatalf("river start: %v", err)
	}
	defer riverClient.Stop(ctx)

	// Handlers
	inboxH      := handlers.NewInboxHandler(pool, waClient, hub)
	contactsH   := handlers.NewContactsHandler(pool)
	tmplsH      := handlers.NewTemplatesHandler(pool, waClient)
	cmpgnsH     := handlers.NewCampaignHandler(pool, riverClient, hub)
	automationH := handlers.NewAutomationHandler(pool)
	analyticsH  := handlers.NewAnalyticsHandler(pool)
	dashboardH  := handlers.NewDashboardHandler(pool)
	clicksH     := handlers.NewClickHandler(pool)
	jwtSecret   := []byte(cfg.JWTSecret)
	authH       := handlers.NewAuthHandler(pool, jwtSecret, cfg.BaseURL)
	teamH       := handlers.NewTeamHandler(pool, jwtSecret, cfg.BaseURL)
	settingsH   := handlers.NewSettingsHandler(pool)
	onboardingH := handlers.NewOnboardingHandler(pool, jwtSecret, cfg.WA.PhoneNumberID, cfg.WA.AccessToken)

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// ── Static assets & public infrastructure ────────────────────────────────
	staticFS, err := fs.Sub(static.FS, ".")
	if err != nil {
		log.Fatalf("static fs: %v", err)
	}
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
	r.Get("/health", handlers.Health(pool))
	r.Get("/webhook",  handlers.WebhookVerify(cfg.WA.WebhookVerifyToken))
	r.Post("/webhook", handlers.WebhookReceive(proc, inboxH, pool, []byte(cfg.WA.AppSecret)))

	// ── Auth & onboarding (public) ────────────────────────────────────────────
	authH.Mount(r)
	r.Route("/onboarding", func(r chi.Router) {
		onboardingH.Mount(r)
	})

	// ── Click tracking (public) ───────────────────────────────────────────────
	r.Route("/c", func(r chi.Router) {
		clicksH.Mount(r)
	})

	// firstRunCheck: redirect to /onboarding when no agents exist.
	firstRunCheck := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n, err := db.CountAgents(r.Context(), pool)
			if err == nil && n == 0 {
				http.Redirect(w, r, "/onboarding", http.StatusSeeOther)
				return
			}
			next.ServeHTTP(w, r)
		})
	}

	// ── WebSocket (needs auth but not RBAC) ───────────────────────────────────
	r.With(mw.RequireAuth(jwtSecret)).Get("/ws", hub.ServeWS)

	// ── All authenticated routes ──────────────────────────────────────────────
	r.Group(func(r chi.Router) {
		r.Use(firstRunCheck)
		r.Use(mw.RequireAuth(jwtSecret))

		// Dashboard
		r.Get("/", dashboardH.Page)

		// Inbox — all roles
		r.Route("/inbox", func(r chi.Router) {
			inboxH.Mount(r)
		})

		// Analytics — all roles (agents see own stats only; enforced in queries)
		r.Route("/analytics", func(r chi.Router) {
			analyticsH.Mount(r)
		})

		// Manager+ routes
		r.Group(func(r chi.Router) {
			r.Use(mw.RequireRole("admin", "manager"))
			r.Route("/contacts", func(r chi.Router) {
				contactsH.Mount(r)
			})
			r.Route("/templates", func(r chi.Router) {
				tmplsH.Mount(r)
			})
			r.Route("/campaigns", func(r chi.Router) {
				cmpgnsH.Mount(r)
			})
			r.Route("/automation", func(r chi.Router) {
				automationH.Mount(r)
			})
		})

		// Admin-only routes
		r.Group(func(r chi.Router) {
			r.Use(mw.RequireRole("admin"))
			r.Route("/team", func(r chi.Router) {
				teamH.Mount(r)
			})
			r.Route("/settings", func(r chi.Router) {
				settingsH.Mount(r)
			})
		})
	})

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: r,
	}

	serverErr := make(chan error, 1)
	go func() {
		log.Printf("listening on :%s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		log.Fatalf("server error: %v", err)
	case sig := <-stop:
		log.Printf("received %s, shutting down gracefully", sig)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown failed, forcing close: %v", err)
		_ = srv.Close()
	}
	log.Println("http server stopped")
	// defer riverClient.Stop(ctx) and defer pool.Close() run next (LIFO order).
}
