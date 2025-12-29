## Audit Trail Library (Go)

Lightweight helper to record audit trail events from Go apps into a database using `database/sql`.

### Features
- Simple API: `InitFromEnv` + `Record` with GCP Pub/Sub.
- Publisher/consumer helpers for Pub/Sub-style queues (advanced/manual wiring).
- Can auto-create the table via `EnsureTable`.
- Auto-detects Postgres (`$1`) vs MySQL/SQLite (`?`) placeholders.
- Minimal dependency: official GCP Pub/Sub client.

### Install
Use the module path of this repo:
```sh
go get github.com/ahsansandiah/audit-trail
```

### Usage
Quick example (init from env, then call `Record`):
```go
package main

import (
    "context"
    "log"

    _ "github.com/jackc/pgx/v5/stdlib"
    "github.com/ahsansandiah/audit-trail"
)

func main() {
    ctx := context.Background()
    if err := audittrail.InitFromEnv(ctx); err != nil {
        log.Fatal(err)
    }
    defer audittrail.Shutdown(ctx)

    entry := audittrail.Entry{
        RequestID: "req-001",
        Actor:     "user-123",
        Action:    "login",
        Endpoint:  "/api/login",
        Request:   map[string]any{"username": "john"},
        IPAddress: "192.168.1.1",
        CreatedBy: "service-a",
    }

    if err := audittrail.Record(ctx, entry); err != nil {
        log.Fatal(err)
    }
}
```

Default env values (override in your service):
- `AUDIT_GCP_PROJECT`: `local-project`
- `AUDIT_PUBSUB_TOPIC`: `audit-trail`
- `AUDIT_PUBSUB_SUBSCRIPTION`: `audit-trail-sub`
- `AUDIT_DB_DRIVER`: `pgx`
- `AUDIT_DB_DSN`: `postgres://user:pass@localhost:5432/audittrail?sslmode=disable`
- `AUDIT_TABLE`: `audit_trail`

Note:
- your service must import the DB driver (e.g., `pgx`) so `database/sql` can open the connection.
- GCP Pub/Sub uses Application Default Credentials (ADC); set it up in your runtime environment.

### Examples
- Pub/Sub flow with DB persistence: `cd examples/basic && GOCACHE=$(pwd)/.cache go run .`
- HTTP middleware wrapper (request-only): `cd examples/httpmiddleware && GOCACHE=$(pwd)/.cache go run .`
- External Pub/Sub mock: `cd examples/external && GOCACHE=$(pwd)/.cache go run .`

### HTTP middleware / decorator
Wrap your `net/http` handlers so every request is published automatically:
```go
_ = audittrail.InitFromEnv(context.Background())
middleware := audittrail.HTTPMiddleware(
    audittrail.RecorderFunc(audittrail.Record),
    audittrail.WithActorHeader("X-User-Id"), // optional overrides
    audittrail.WithRequestIDHeader("X-Request-Id"),
)

mux := http.NewServeMux()
mux.HandleFunc("/api/orders", func(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusCreated)
    w.Write([]byte("ok"))
})

http.ListenAndServe(":8080", middleware(mux))
```
Defaults:
- Action: `"METHOD /path"` and Endpoint: request path.
- Request ID header: `X-Request-Id`, Actor header: `X-User-Id`, IP header: `X-Forwarded-For`.
- Response payload: not captured by default (use `WithResponsePayload` if needed).
You can customize the request payload (`WithRequestPayload`), action builder (`WithAction`), error handler (`WithErrorHandler`), and clock (`WithNow`).

### Pub/Sub consumer
Use the consumer to persist entries from your queue into the database:
```go
consumer, _ := audittrail.NewConsumer(audit, subscriber, nil)
if err := consumer.Run(context.Background()); err != nil {
    log.Printf("consumer stopped: %v", err)
}
```

### Configuration
- `Config.TableName`: default `audit_trail`.
- `Config.Placeholder`: override placeholder style (`audittrail.PlaceholderQuestion` or `audittrail.PlaceholderDollar`) if auto-detect does not fit your driver.
- Use `audittrail.NewAuditTrail` to initialize.

### License
MIT.
