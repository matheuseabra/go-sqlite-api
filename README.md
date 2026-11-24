# Go + SQLite for focused services

This repository makes the case for a small, operationally calm API stack: one Go binary, SQLite in WAL mode, no ORM, no cache tier, no broker, and no container requirement. Static assets and SQL migrations ship inside the executable. Litestream can continuously copy the SQLite state to S3-compatible object storage such as Cloudflare R2.

It is not a claim that SQLite replaces PostgreSQL everywhere. It is a concrete reference for the large middle ground where one well-run machine is the simplest system that meets the product's real needs.

## The case for this stack

Every separate service has a cost: configuration, credentials, network failure modes, upgrades, dashboards, connection pools, and incident response. For a single-region product with modest write contention, embedding SQLite removes most of that surface area without giving up SQL, transactions, indexes, backups, or a straightforward path to scale later.

Go adds predictable concurrency and a small deployment footprint. SQLite keeps data local to the process instead of crossing a network hop for each query. The binary includes its schema and UI, so a deploy is a replaceable executable plus a durable data directory.

The project uses a pure-Go SQLite driver. `CGO_ENABLED=0` builds a statically linked Linux binary.

## A good fit

- Independent SaaS products and paid developer tools with one primary region.
- Internal operations tools, customer portals, back-office APIs, and workflow systems.
- Webhook consumers and API gateways where most requests read configuration or route state.
- A new product that needs boring backups and fast iteration before its workload proves that distributed infrastructure is necessary.
- Single-tenant or partitioned deployments where each customer can own a small durable database.

## Use another architecture when

- Several active application nodes must write to the same database with low coordination latency.
- The core workload requires thousands of immediately durable, strongly consistent writes every second.
- The database is hundreds of GB, or full-text/vector search is central to the product.
- Cross-region writes, multi-primary availability, or independently scalable data services are requirements rather than future possibilities.

In those cases, use the operational complexity deliberately: PostgreSQL, a queue, a cache, or a managed data platform may be the better fit.

## How this reference works

- `ReadDB` uses `mode=ro&_query_only=on` and five connections for concurrent reads.
- `WriteDB` uses one connection, `_txlock=immediate`, and a Go mutex around every transaction.
- SQLite starts in WAL mode with `synchronous=NORMAL`, 128 MiB mmap, a 32 MiB page cache, and `wal_autocheckpoint=0`; Litestream owns checkpoint and replica behavior.
- `internal/hot` keeps configuration, route snapshots, local fixed-window counters, and balances in memory. Account changes use fine-grained locks.
- `internal/persist` accepts 8,192 events and commits batches every 15 ms or at 64 events. On shutdown it stops admission and drains the queue.
- HTTP responses set basic defensive headers, use bounded request bodies, reject unknown JSON fields, and run under server read/write/header timeouts.

## Important boundaries

The charge endpoint demonstrates an asynchronous reservation-plus-persistence flow. It is **not** a complete payments or accounting system. Before using an equivalent path for money, subscriptions, credits, or entitlements, add authenticated ownership, durable idempotency, reconciliation, audit controls, and an explicit recovery policy for the interval between memory reservation and a durable commit.

Likewise, the in-process rate limit is process-local. Put authentication, TLS, request-size policy, and distributed rate limiting at the edge when your threat model needs them.

## Run locally

```sh
go mod tidy
go test ./...
go vet ./...
go run ./cmd/api
curl http://localhost:8080/healthz
```

The service creates `./data/api.db` by default. That directory is ignored by Git. Provision durable account balances through your own authenticated control plane before submitting charges. Startup reloads durable balances into the hot cache.

## Build and deploy

```sh
CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o go-sqlite-api ./cmd/api
sudo install -m 0755 go-sqlite-api /usr/local/bin/go-sqlite-api
sudo install -m 0644 deploy/go-sqlite-api.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now go-sqlite-api
```

Configure Litestream with [deploy/litestream.yml](deploy/litestream.yml) and provide R2 credentials through a root-readable environment file. Do not commit credentials. [scripts/restore.sh](scripts/restore.sh) is the explicit operator restore helper; it requires `LITESTREAM_CONFIG` and `API_DB_PATH`.

## Contributing and security

See [CONTRIBUTING.md](CONTRIBUTING.md) for development expectations and [SECURITY.md](SECURITY.md) for responsible disclosure. This project is released under the [MIT License](LICENSE).
