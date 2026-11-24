// Package config reads the runtime configuration from the environment.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
)

type Config struct {
	BotToken  string
	AppURL    string
	HTTPAddr  string
	DBPath    string
	Uploads   string
	Locations map[string]string
}

// Load reads the environment and the locations file named by LOCATIONS_PATH.
func Load() (Config, error) {
	cfg := Config{
		BotToken: os.Getenv("TELEGRAM_BOT_TOKEN"),
		AppURL:   os.Getenv("APP_URL"),
		HTTPAddr: env("HTTP_ADDR", ":8080"),
		DBPath:   env("DB_PATH", "santa-mini.db"),
		Uploads:  env("UPLOADS_PATH", "uploads"),
	}
	parsed, err := url.Parse(cfg.AppURL)
	if cfg.BotToken == "" || err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return Config{}, errors.New("set TELEGRAM_BOT_TOKEN and a public HTTPS APP_URL")
	}
	cfg.Locations, err = loadLocations(env("LOCATIONS_PATH", "locations.json"))
	return cfg, err
}

func loadLocations(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	locations := map[string]string{}
	if err := json.Unmarshal(data, &locations); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(locations) == 0 {
		return nil, errors.New("locations cannot be empty")
	}
	return locations, nil
}

func env(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}
