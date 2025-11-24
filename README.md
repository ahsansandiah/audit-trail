## Audit Trail Library (Go)

Lightweight helper to record audit trail events from Go apps into a database using `database/sql`.

### Features
- Simple API: create an `AuditTrail`, call `AuditTrail` with your entry.
- Can auto-create the table via `EnsureTable`.
- Auto-detects Postgres (`$1`) vs MySQL/SQLite (`?`) placeholders.
- Zero external deps beyond the standard library.

### Install
Use the module path of this repo:
```sh
go get github.com/ahsansandiah/audit-trail
```

### Usage
Quick example (Postgres):
```go
package main

import (
    "context"
    "database/sql"
    "log"

    _ "github.com/jackc/pgx/v5/stdlib"
    "github.com/ahsansandiah/audit-trail"
)

func main() {
    db, err := sql.Open("pgx", "postgres://user:pass@localhost:5432/dbname")
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close()

    audit, err := audittrail.NewAuditTrail(audittrail.Config{DB: db})
    if err != nil {
        log.Fatal(err)
    }

    ctx := context.Background()
    // Optional: ensure table exists (or create via migration)
    if err := audit.EnsureTable(ctx); err != nil {
        log.Fatal(err)
    }

    entry := audittrail.Entry{
        RequestID: "req-001",
        Actor:     "user-123",
        Action:    "login",
        Endpoint:  "/api/login",
        Request:   map[string]any{"username": "john"},
        Response:  map[string]any{"status": "ok"},
        IPAddress: "192.168.1.1",
        CreatedBy: "service-a",
    }

    if err := audit.Record(ctx, entry); err != nil {
        log.Fatal(err)
    }
}
```

### Configuration
- `Config.TableName`: default `audit_trail`.
- `Config.Placeholder`: override placeholder style (`audittrail.PlaceholderQuestion` or `audittrail.PlaceholderDollar`) if auto-detect does not fit your driver.
- Use `audittrail.NewAuditTrail` to initialize.

### License
MIT.
