package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/DmitryKolbin/santa-tg-web/internal/mini"
	_ "github.com/uptrace/bun/driver/sqliteshim"
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
	if token == "" || err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return errors.New("set TELEGRAM_BOT_TOKEN and a public HTTPS root APP_URL")
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
	db, err := sql.Open("sqliteshim", env("DB_PATH", "santa-mini.db"))
	if err != nil {
		return err
	}
	defer db.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	bot := mini.NewBot(token, appURL)
	name, err := bot.Configure(ctx)
	if err != nil {
		return err
	}
	app, err := mini.New(db, token, appURL, name, env("UPLOADS_PATH", "uploads"), locations, bot.Notify)
	if err != nil {
		return err
	}
	server := &http.Server{Addr: env("HTTP_ADDR", ":8080"), Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 60 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	go bot.Run(ctx)
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			slog.Error("shutdown", "error", err)
		}
	}()
	slog.Info("Secret Santa listening", "address", server.Addr)
	err = server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
func main() {
	if err := run(); err != nil {
		slog.Error("startup failed", "error", err)
		os.Exit(1)
	}
}
