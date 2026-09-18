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

	"coveragecheck/internal/api"
	"coveragecheck/internal/chatbot"
	"coveragecheck/internal/config"
	"coveragecheck/internal/db"
	"coveragecheck/internal/events"
	"coveragecheck/internal/llm"
	"coveragecheck/internal/notify"
	"coveragecheck/internal/queue"
	"coveragecheck/internal/ratelimit"
	"coveragecheck/internal/stedi"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cfg, err := config.Load()
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("db connect", "err", err)
		os.Exit(1)
	}
	defer store.Pool.Close()

	var client stedi.Client
	if cfg.StediMode == "live" {
		client = stedi.NewLive(cfg.StediBaseURL, cfg.StediAPIKey)
	} else {
		client = &stedi.MockClient{LatencyMin: cfg.MockLatencyMin, LatencyMax: cfg.MockLatencyMax, FailRate: cfg.MockFailRate}
	}
	gen := llm.New(cfg.OpenRouterAPIKey, cfg.LLMModel)
	limiter := ratelimit.New(cfg.PayerRPS, cfg.PayerBurst)

	var sender notify.Sender
	switch {
	case cfg.ResendAPIKey != "":
		sender = notify.NewResend(cfg.ResendAPIKey, cfg.ResendFrom)
	case cfg.SMTPHost != "":
		sender = notify.NewSMTP(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPUser, cfg.SMTPPass, cfg.SMTPFrom)
	}
	geminiClient := chatbot.NewWithBaseURL(cfg.GeminiAPIKey, cfg.GeminiModel, cfg.GeminiBaseURL, cfg.GeminiRPM)
	assistant := chatbot.NewAssistant(geminiClient, store, log)

	q, err := queue.New(store.Pool, &queue.Deps{Cfg: cfg, Store: store, Stedi: client, LLM: gen, Limiter: limiter, Log: log},
		&queue.NoticeDeps{Sender: sender, PracticePhone: cfg.PracticePhone})
	if err != nil {
		log.Error("queue", "err", err)
		os.Exit(1)
	}
	if err := q.Start(ctx); err != nil {
		log.Error("queue start", "err", err)
		os.Exit(1)
	}

	hub := events.New(store, limiter, log)
	go hub.Run(ctx, store.Pool)

	srv, err := api.New(cfg, store, q, hub, limiter, gen, sender, assistant, client.Mode(), log)
	if err != nil {
		log.Error("api", "err", err)
		os.Exit(1)
	}
	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		loc = time.Local
	}
	srv.Nightly().StartTicker(ctx, cfg.NightlyHour, loc)
	provider := "none (notices logged in-app)"
	if cfg.ResendAPIKey != "" {
		provider = "resend"
	} else if cfg.SMTPHost != "" {
		provider = "smtp:" + cfg.SMTPHost
	}
	log.Info("pre-visit notices", "email_provider", provider, "nightly_hour", cfg.NightlyHour, "tz", loc.String())
	log.Info("in-app assistant", "configured", assistant.Configured(), "model", cfg.GeminiModel, "gemini_rpm", cfg.GeminiRPM, "chat_rpm", cfg.ChatRPM)

	httpSrv := &http.Server{Addr: ":" + cfg.Port, Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}

	go func() {
		log.Info("Verafy API listening", "port", cfg.Port, "stedi", client.Mode(), "llm", gen.Enabled(), "workers", cfg.MaxWorkers, "payerRPS", cfg.PayerRPS)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
	_ = q.Stop(shutdownCtx)
}
