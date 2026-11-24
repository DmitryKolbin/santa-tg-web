package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/DmitryKolbin/santa-tg-web/internal/api"
	"github.com/DmitryKolbin/santa-tg-web/internal/config"
	"github.com/DmitryKolbin/santa-tg-web/internal/santa"
	"github.com/DmitryKolbin/santa-tg-web/internal/store"
	"github.com/DmitryKolbin/santa-tg-web/internal/telegram"
)

func main() {
	if err := run(); err != nil {
		slog.Error("startup failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	sqldb, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer sqldb.Close()
	db := store.New(sqldb)
	if err := store.Migrate(context.Background(), db); err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.Uploads, 0700); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	bot := telegram.NewBot(cfg.BotToken, cfg.AppURL)
	botName, err := bot.Configure(ctx)
	if err != nil {
		return err
	}
	svc := santa.New(store.NewStore(db), santa.Options{Uploads: cfg.Uploads, BotName: botName, Locations: cfg.Locations, Notifier: bot})
	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           api.New(svc, cfg.BotToken, cfg.AppURL),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}

	botCtx, stopBot := context.WithCancel(context.Background())
	defer stopBot()
	go bot.Run(botCtx)

	slog.Info("Secret Santa listening", "address", server.Addr)
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.ListenAndServe() }()
	select {
	case err = <-serveErr:
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
			return fmt.Errorf("shutdown: %w", err)
		}
		stopBot()
		err = <-serveErr
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
