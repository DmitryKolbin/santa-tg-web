package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/DmitryKolbin/santa-tg-web/internal/api"
	"github.com/DmitryKolbin/santa-tg-web/internal/mini"
	"github.com/DmitryKolbin/santa-tg-web/internal/santa"
	"github.com/DmitryKolbin/santa-tg-web/internal/store"
)

func env(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}
func run() error {
	token, appURL := os.Getenv("TELEGRAM_BOT_TOKEN"), os.Getenv("APP_URL")
	parsed, err := url.Parse(appURL)
	if token == "" || err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("set TELEGRAM_BOT_TOKEN and a public HTTPS APP_URL")
	}
	locationsData, err := os.ReadFile(env("LOCATIONS_PATH", "locations.json"))
	if err != nil {
		return err
	}
	locations := map[string]string{}
	if err := json.Unmarshal(locationsData, &locations); err != nil {
		return err
	}
	if len(locations) == 0 {
		return errors.New("locations cannot be empty")
	}
	db, err := store.Open(env("DB_PATH", "santa-mini.db"))
	if err != nil {
		return err
	}
	defer db.Close()
	bunDB := store.New(db)
	if err := store.Migrate(context.Background(), bunDB); err != nil {
		return err
	}
	uploads := env("UPLOADS_PATH", "uploads")
	if err := os.MkdirAll(uploads, 0700); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	bot := mini.NewBot(token, appURL)
	name, err := bot.Configure(ctx)
	if err != nil {
		return err
	}
	svc := santa.New(store.NewStore(bunDB), santa.Options{Uploads: uploads, BotName: name, Locations: locations, Notifier: bot})
	server := &http.Server{Addr: env("HTTP_ADDR", ":8080"), Handler: api.New(svc, token, appURL), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 60 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	botCtx, stopBot := context.WithCancel(context.Background())
	defer stopBot()
	go bot.Run(botCtx)
	slog.Info("Secret Santa listening", "address", server.Addr)
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.ListenAndServe() }()
	select {
	case err = <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
			return fmt.Errorf("shutdown: %w", err)
		}
		stopBot()
		err = <-serveErr
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
func main() {
	if err := run(); err != nil {
		slog.Error("startup failed", "error", err)
		os.Exit(1)
	}
}
