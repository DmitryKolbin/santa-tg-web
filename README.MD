# Secret Santa backend

Go API and Telegram bot for the Secret Santa Mini App. The frontend lives in a separate repository and is published as static files on GitHub Pages.

## Deploy on a VPS

Requirements: a Telegram bot token, a public HTTPS frontend URL, and an HTTPS reverse proxy forwarding the API domain to `127.0.0.1:8080`.

Build the image from `Dockerfile`, provide `TELEGRAM_BOT_TOKEN` and `APP_URL`,
publish container port 8080 only on `127.0.0.1`, and mount persistent storage at
`/data`. The container image supplies the remaining runtime defaults.

Configure your existing nginx, Caddy, Traefik, or other HTTPS proxy to forward the API domain to `http://127.0.0.1:8080`. Do not add Basic Auth because the API authenticates signed Telegram data through the `Authorization` header.

The backend permits browser API requests only from the origin of `APP_URL`. For `https://user.github.io/repo/`, the allowed origin is `https://user.github.io`.

Check `https://<api-domain>/healthz` for `ok`. The bot configures its menu button with `APP_URL` and uses long polling, so its webhook must be disabled. SQLite and attachments are stored under `/data`; run one application instance.

## Development

Requires Go 1.23.3+.

The backend uses Bun with SQLite. A request goes `api` → `santa` → `store`:

| Package | Contents |
| --- | --- |
| `internal/api` | HTTP routes, Telegram init data auth, CORS, JSON encoding, error statuses |
| `internal/santa` | Business rules: games, invites, profiles, the draw, chats, deliveries, attachments |
| `internal/store` | Bun models, migrations and all SQL queries; `Tx` for transactions |
| `internal/telegram` | Bot API client, notifications and long polling |
| `internal/config` | Environment variables and `locations.json` |
| `internal/migrateold` | Import from the old bot database |

`cmd/app/main.go` wires these together.

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
mockery
ginkgo -r
go test -race ./...
CGO_ENABLED=0 go build ./cmd/app
docker build .
```

Tests use Ginkgo v2 and Gomega. The API tests run the whole HTTP flow against a
temporary SQLite database. The only mock is the Telegram HTTP client, generated
by mockery from `.mockery.yaml` under `mocks/`.

## Import from the old bot

Stop the old bot and make a backup of its SQLite database. Import that backup into a new file:

```sh
go run ./cmd/migrate-old --source /path/to/old.db --target /path/to/santa-mini.db
```

The target must not exist or must be empty. The command imports profiles, games, members, and completed draws. A started game with a missing or invalid route is imported as not started and reported as a warning. Keep the old database until the imported application has been verified.

## License

MIT
