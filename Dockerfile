FROM golang:1.26-alpine AS backend
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 go test ./... && CGO_ENABLED=0 go build -trimpath -o /santa ./cmd/app

FROM alpine:3.23
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 santa && mkdir -p /data/uploads && chown -R santa:santa /data
WORKDIR /app
COPY --from=backend /santa /app/santa
COPY locations.json ./locations.json
ENV HTTP_ADDR=:8080 DB_PATH=/data/santa-mini.db UPLOADS_PATH=/data/uploads
USER santa
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["/app/santa"]
