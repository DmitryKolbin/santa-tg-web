# Secret Santa backend

Go API and Telegram bot for the Secret Santa Mini App. The frontend lives in a separate repository and is published as static files on GitHub Pages.

## Deploy on a VPS

Requirements: a Telegram bot token, a public HTTPS frontend URL, and an HTTPS reverse proxy forwarding the API domain to `127.0.0.1:8080`.

Copy `.env.example` to `.env` and fill in:

| Variable | Value |
| --- | --- |
| `TELEGRAM_BOT_TOKEN` | Bot token from BotFather |
| `APP_URL` | Full Pages URL, for example `https://user.github.io/santa-tg-frontend/` |
| `HTTP_HOST` | Published interface; defaults to `127.0.0.1` |
| `HTTP_PORT` | Published port; defaults to `8080` |

```sh
docker compose config --quiet
docker compose up --build -d
```

Configure your existing nginx, Caddy, Traefik, or other HTTPS proxy to forward the API domain to `http://127.0.0.1:8080`. Do not add Basic Auth because the API authenticates signed Telegram data through the `Authorization` header.

The backend permits browser API requests only from the origin of `APP_URL`. For `https://user.github.io/repo/`, the allowed origin is `https://user.github.io`.

Check `https://<api-domain>/healthz` for `ok`. The bot configures its menu button with `APP_URL` and uses long polling, so its webhook must be disabled. SQLite and attachments are stored in the `santa-data` volume; run one application instance.

## Development

Requires Go 1.23.3+.

```sh
go test ./...
TELEGRAM_BOT_TOKEN=... APP_URL=https://user.github.io/santa-tg-frontend/ go run ./cmd/app
```

Optional runtime variables:

| Variable | Default |
| --- | --- |
| `HTTP_ADDR` | `:8080` |
| `DB_PATH` | `santa-mini.db` locally, `/data/santa-mini.db` in Docker |
| `UPLOADS_PATH` | `uploads` locally, `/data/uploads` in Docker |
| `LOCATIONS_PATH` | `locations.json` |

## Checks

```sh
go test -race ./...
CGO_ENABLED=0 go build ./cmd/app
docker build .
```

## License

MIT
