# Audit Trail untuk Gin Framework

## Database Schema

```sql
CREATE TABLE audit_trail (
    log_aduit_trail_id VARCHAR(64) PRIMARY KEY,
    log_req_id         VARCHAR(128),
    log_action         VARCHAR(255) NOT NULL,
    log_endpoint       TEXT,
    log_request        JSON,
    log_response       JSON,
    log_created_date   TIMESTAMP NOT NULL,
    log_created_by     VARCHAR(255)  -- User ID yang create/update/delete
);
```

## Installation

```bash
go get github.com/ahsansandiah/audit-trail
go get github.com/gin-gonic/gin
```

## Environment Variables

```bash
AUDIT_AUTO_INIT=true
AUDIT_GCP_PROJECT=my-project
AUDIT_PUBSUB_TOPIC=audit-trail
AUDIT_PUBSUB_SUBSCRIPTION=audit-trail-sub
AUDIT_DB_DRIVER=pgx
AUDIT_DB_DSN=postgres://user:pass@localhost:5432/audittrail?sslmode=disable
AUDIT_TABLE=audit_trail
```

## Quick Start

### 1. Setup Middleware (Sekali Saja)

```go
package main

import (
    "context"
    "github.com/gin-gonic/gin"
    "github.com/ahsansandiah/audit-trail"
)

func main() {
    // Init
    audittrail.InitFromEnv(context.Background())
    defer audittrail.Shutdown(context.Background())

    r := gin.Default()

    // Setup audit middleware SEKALI
    r.Use(audittrail.GinMiddleware(
        audittrail.WithServiceName("order-service"),
        audittrail.WithSkipPaths("/health", "/login"),
    ))

    // Setup auth middleware untuk set user_id
    r.Use(authMiddleware())

    // Routes
    r.POST("/orders", handleCreateOrder)
    r.Run(":8080")
}
```

### 2. Auth Middleware (Set User ID)

```go
func authMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        token := c.GetHeader("Authorization")

        // Validate JWT token
        userID, err := validateJWT(token)
        if err != nil {
            c.AbortWithStatusJSON(401, gin.H{"error": "unauthorized"})
            return
        }

        // Set user_id - ini yang akan di-capture sebagai log_created_by
        c.Set("user_id", userID)

        c.Next()
    }
}
```

### 3. Handler (Tidak Perlu Kode Audit)

```go
func handleCreateOrder(c *gin.Context) {
    var req CreateOrderRequest
    c.ShouldBindJSON(&req)

    // Optional: custom action name
    c.Set("audit_action", "CREATE_ORDER")

    // Business logic saja
    order := createOrder(req)

    c.JSON(201, order)

    // Audit OTOMATIS ter-record!
}
```

## Hasil Audit di Database

```sql
SELECT * FROM audit_trail ORDER BY log_created_date DESC LIMIT 5;
```

| log_aduit_trail_id | log_req_id | log_action | log_endpoint | log_request | log_created_date | log_created_by |
|--------------------|------------|------------|--------------|-------------|------------------|----------------|
| abc123 | req-001 | CREATE_ORDER | /orders | {"product_id":"p1","qty":2} | 2025-01-05 10:30:00 | user-123 |
| def456 | req-002 | UPDATE_ORDER | /orders/789 | {"status":"completed"} | 2025-01-05 10:35:00 | user-123 |
| ghi789 | req-003 | DELETE_ORDER | /orders/789 | null | 2025-01-05 10:40:00 | user-456 |

## Query Examples

### Siapa yang create order tertentu?

```sql
SELECT log_created_by, log_created_date, log_request
FROM audit_trail
WHERE log_action = 'CREATE_ORDER'
  AND log_request::jsonb @> '{"product_id": "p1"}'
ORDER BY log_created_date DESC;
```

### Track semua aktivitas user tertentu

```sql
SELECT log_action, log_endpoint, log_created_date
FROM audit_trail
WHERE log_created_by = 'user-123'
ORDER BY log_created_date DESC;
```

### Siapa yang delete data?

```sql
SELECT log_created_by, log_endpoint, log_created_date
FROM audit_trail
WHERE log_action = 'DELETE_ORDER'
  AND log_endpoint = '/orders/789';
```

## Configuration Options

```go
r.Use(audittrail.GinMiddleware(
    // Service name (untuk log_created_by jika user tidak ada)
    audittrail.WithServiceName("order-service"),

    // Skip paths (tidak di-audit)
    audittrail.WithSkipPaths("/health", "/login", "/register"),

    // Enable/disable request body capture
    audittrail.WithCaptureRequestBody(true),

    // Max body size (default 1MB)
    audittrail.WithMaxBodySize(1024 * 1024),

    // Custom user extraction
    audittrail.WithUserExtractor(func(c *gin.Context) string {
        if uid, exists := c.Get("user_id"); exists {
            return uid.(string)
        }
        return "anonymous"
    }),
))
```

## Custom Action Names

```go
func handleCreateOrder(c *gin.Context) {
    // Set custom action name (optional)
    c.Set("audit_action", "CREATE_ORDER")

    // Default: "POST /orders"
    // Custom: "CREATE_ORDER"
}
```

## Framework Agnostic

Library ini framework-agnostic. Kalau ganti dari Gin ke Echo/Fiber:

```go
// Dari Gin:
import "github.com/ahsansandiah/audit-trail"
r.Use(audittrail.GinMiddleware(...))

// Ke Echo (tinggal ganti middleware):
import "github.com/ahsansandiah/audit-trail"
e.Use(audittrail.EchoMiddleware(...))  // Future: akan dibuat
```

Core library tetap sama, cuma adapter yang berbeda.

## Testing

Disable audit saat testing:

```bash
# Jangan set AUDIT_AUTO_INIT
# atau set ke false
AUDIT_AUTO_INIT=false go test ./...
```

## Architecture

```
Service (Gin)
    ↓
Gin Adapter (thin layer, 20 lines)
    ↓
Core Library (framework agnostic)
    ↓
Pub/Sub → Consumer → Database
```

## Benefits

✅ **Zero boilerplate** - setup sekali, jalan untuk semua routes
✅ **Automatic capture** - user ID, request, response otomatis
✅ **Non-blocking** - async via Pub/Sub, tidak ganggu performa
✅ **Framework agnostic** - mudah ganti framework
✅ **Database schema match** - sesuai dengan tabel existing
