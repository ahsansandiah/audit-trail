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

### Environment Variables
Required environment variables for database connection:
- `DATABASE_USER`: Database username (default: `postgres`)
- `DATABASE_PASSWORD`: Database password (default: `postgres`)
- `DATABASE_HOST`: Database host (default: `localhost`)
- `DATABASE_PORT`: Database port (default: `5432`)
- `DATABASE_NAME`: Database name (default: `audittrail`)
- `AUDIT_GCP_PROJECT`: GCP Project ID (default: `local-project`)

### Hardcoded Values
The following values are hardcoded in the library:
- Database Driver: `pgx` (PostgreSQL)
- Audit Trail Table: `log_audit_trail`
- User Login Activity Table: `log_user_login_activity`
- Pub/Sub Topic: `audit-trail`
- Pub/Sub Subscription: `audit-trail-sub`

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

### User Login Activity Tracking
Track user login sessions and link audit trail entries to specific login activities:

```go
// Record login when user authenticates
loginActivityID, err := audittrail.RecordLogin(ctx, audittrail.UserLoginActivity{
    UserID:       "user-123",
    IPAddress:    "192.168.1.1",
    UserAgent:    "Mozilla/5.0...",
    DeviceInfo:   "Chrome on Windows",
    Location:     "Jakarta, Indonesia",
    SessionToken: "session-token-abc",
})

// The loginActivityID can be:
// 1. Stored in JWT claims
// 2. Stored in session/redis
// 3. Returned to client to send in X-User-Login-Activity-Id header

// Record logout when user logs out
err = audittrail.RecordLogout(ctx, loginActivityID)

// Get login activity by ID
activity, err := audittrail.GetLoginActivity(ctx, loginActivityID)

// Get login activity by session token
activity, err := audittrail.GetLoginActivityByToken(ctx, "session-token-abc")
```

The `user_login_activity_id` field in audit trail entries allows you to:
- Track all actions performed during a specific login session
- Identify which device/location was used for each action
- Detect anomalies by correlating actions with login metadata

#### Gin Middleware Integration
The Gin middleware automatically extracts `user_login_activity_id` from context or header:

```go
// In your auth middleware, set the login activity ID
c.Set("user_login_activity_id", loginActivityID)

// Or clients can send it via header
// X-User-Login-Activity-Id: <activity-id>

// Custom extractor (optional)
audittrail.GinMiddleware(
    audittrail.WithLoginActivityExtractor(func(c *gin.Context) string {
        // Custom logic to extract login activity ID
        return c.GetHeader("X-Session-Id")
    }),
)
```

### Configuration
- `Config.TableName`: default `audit_trail`.
- `Config.Placeholder`: override placeholder style (`audittrail.PlaceholderQuestion` or `audittrail.PlaceholderDollar`) if auto-detect does not fit your driver.
- Use `audittrail.NewAuditTrail` to initialize.

### License
MIT.
