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

	appdb "github.com/DmitryKolbin/santa-tg-web/internal/database"
	"github.com/DmitryKolbin/santa-tg-web/internal/mini"
	"github.com/DmitryKolbin/santa-tg-web/internal/santa/controllers"
	"github.com/DmitryKolbin/santa-tg-web/internal/santa/httpapi"
	"github.com/DmitryKolbin/santa-tg-web/internal/santa/repos"
	"github.com/DmitryKolbin/santa-tg-web/internal/santa/services"
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
	db, err := appdb.Open(env("DB_PATH", "santa-mini.db"))
	if err != nil {
		return err
	}
	defer db.Close()
	bunDB := appdb.New(db)
	if err := appdb.Migrate(context.Background(), bunDB); err != nil {
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
	store := repos.New(bunDB)
	meService := services.NewMeService(store, locations, bot)
	locationService := services.NewLocationService(store, locations)
	gameService := services.NewGameService(store, name, locations, bot)
	inviteService := services.NewInviteService(store)
	threadService := services.NewThreadService(store, uploads, bot)
	fileService := services.NewFileService(store, uploads)
	api := httpapi.New(token)
	mux := http.NewServeMux()
	routeControllers := []controllers.Controller{
		controllers.NewMeController(api, meService),
		controllers.NewLocationsController(api, locationService),
		controllers.NewGamesController(api, gameService),
		controllers.NewInvitesController(api, inviteService),
		controllers.NewThreadsController(api, threadService),
		controllers.NewFilesController(api, fileService),
	}
	for _, controller := range routeControllers {
		controller.RegisterRoutes(mux)
	}
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		pingCtx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if err := bunDB.PingContext(pingCtx); err != nil {
			http.Error(w, "unhealthy", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "Not found", http.StatusNotFound) })
	server := &http.Server{Addr: env("HTTP_ADDR", ":8080"), Handler: httpapi.Security(appURL, mux), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 60 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
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
