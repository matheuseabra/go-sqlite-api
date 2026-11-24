# Go SQLite API skeleton

This is a single-machine API baseline. It embeds static files and SQL migrations in one Go binary, uses no ORM or external middleware, and uses the pure-Go `modernc.org/sqlite` driver so a CGO-free static build is possible.

## Architecture

- `ReadDB`: URI contains `mode=ro&_query_only=on`, with five read connections.
- `WriteDB`: one SQLite connection, `_txlock=immediate`, and a Go mutex around every transaction.
- SQLite initialization uses WAL, `synchronous=NORMAL`, 128 MiB mmap, 32 MiB page cache, and `wal_autocheckpoint=0`. Litestream owns checkpoint/sync behavior.
- `internal/hot` holds config and routing snapshots, request counters, and balances in process memory. Account balance changes use per-account locks.
- `internal/persist` accepts up to 8,192 events and commits at 15 ms or 64 events. Shutdown stops admission and drains the queue.

The charge endpoint is deliberately an **asynchronous durability** example: it reserves the in-memory balance, then queues its durable record. For money movement, add request idempotency, authenticated ownership, reconciliation, and a durable recovery policy before production use.

## Run and test

```sh
go mod tidy
go test ./...
go run ./cmd/api
curl http://localhost:8080/healthz
```

Set an initial balance in your own provisioning flow before calling `POST /v1/accounts/{id}/charges`. Startup loads durable balances into the in-memory cache; the skeleton exposes the cached balance at `GET /v1/accounts/{id}`.

## Build and deploy

```sh
CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o go-sqlite-api ./cmd/api
sudo install -m 0755 go-sqlite-api /usr/local/bin/go-sqlite-api
sudo install -m 0644 deploy/go-sqlite-api.service /etc/systemd/system/
sudo systemctl daemon-reload && sudo systemctl enable --now go-sqlite-api
```

Configure Litestream with `deploy/litestream.yml` and provide its R2 credentials through a root-readable environment file. Do not commit credentials. `scripts/restore.sh` is the operator restore helper; it intentionally requires explicit environment variables.
