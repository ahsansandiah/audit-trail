# Audit Trail Example Service

Contoh lengkap implementasi audit trail package di service Anda.

## 📋 Prerequisites

1. **Go 1.21+**
2. **PostgreSQL** (atau database lain yang didukung)
3. **GCP Account** dengan Pub/Sub enabled
4. **GCP Service Account** dengan permissions:
   - `pubsub.publisher` (untuk publish message)
   - `pubsub.subscriber` (untuk consume message)
   - `secretmanager.secretAccessor` (optional - jika pakai Secret Manager)

---

## 🚀 Setup

### 1. Install Dependencies

```bash
go mod init your-service-name
go get github.com/ahsansandiah/audit-trail
go get github.com/gin-gonic/gin

# Install database driver sesuai yang Anda pakai:
# PostgreSQL (pgx):
go get github.com/jackc/pgx/v5

# PostgreSQL (pq) - alternative:
# go get github.com/lib/pq

# MySQL:
# go get github.com/go-sql-driver/mysql

# SQLite:
# go get github.com/mattn/go-sqlite3
```

**Penting:** Edit `ex_service.go` dan uncomment import driver yang sesuai:
```go
// Di ex_service.go, uncomment salah satu:
_ "github.com/jackc/pgx/v5/stdlib"  // PostgreSQL (pgx)
// _ "github.com/lib/pq"               // PostgreSQL (pq)
// _ "github.com/go-sql-driver/mysql"  // MySQL
// _ "github.com/mattn/go-sqlite3"     // SQLite
```

### 2. Setup GCP Pub/Sub

```bash
# Set your GCP project
export PROJECT_ID=your-gcp-project-id
gcloud config set project $PROJECT_ID

# Create Pub/Sub topic
gcloud pubsub topics create audit-trail

# Create subscription
gcloud pubsub subscriptions create audit-trail-sub \
  --topic=audit-trail \
  --ack-deadline=60

# Verify
gcloud pubsub topics list
gcloud pubsub subscriptions list
```

### 3. Setup Database

**Create PostgreSQL database:**

```sql
-- Create database
CREATE DATABASE audit_db;

-- Create user
CREATE USER audit_user WITH PASSWORD 'your_password';

-- Grant permissions
GRANT ALL PRIVILEGES ON DATABASE audit_db TO audit_user;

-- Connect to database
\c audit_db

-- Create table (otomatis dibuat oleh audit trail, atau manual)
CREATE TABLE IF NOT EXISTS audit_trail (
    log_aduit_trail_id VARCHAR(64) PRIMARY KEY,
    log_req_id VARCHAR(128) NULL,
    log_action VARCHAR(255) NOT NULL,
    log_endpoint TEXT NULL,
    log_request JSON NULL,
    log_response JSON NULL,
    log_created_date TIMESTAMP NOT NULL,
    log_created_by VARCHAR(255) NULL
);

-- Create index untuk query performance
CREATE INDEX idx_audit_created_date ON audit_trail(log_created_date);
CREATE INDEX idx_audit_created_by ON audit_trail(log_created_by);
CREATE INDEX idx_audit_action ON audit_trail(log_action);
```

### 4. Setup Environment Variables

```bash
# Copy .env.example
cp .env.example .env

# Edit .env dengan nilai sebenarnya
nano .env
```

**Minimal .env configuration:**

```bash
AUDIT_GCP_PROJECT=your-gcp-project-id
AUDIT_PUBSUB_TOPIC=audit-trail
AUDIT_PUBSUB_SUBSCRIPTION=audit-trail-sub
AUDIT_DB_DRIVER=pgx
AUDIT_DB_DSN=postgres://audit_user:your_password@localhost:5432/audit_db?sslmode=disable
AUDIT_TABLE=audit_trail
GOOGLE_APPLICATION_CREDENTIALS=/path/to/service-account-key.json
```

### 5. GCP Authentication

**Option A: Local Development (Service Account Key)**

```bash
# Download service account key dari GCP Console
# Simpan di: /path/to/service-account-key.json

# Set environment variable
export GOOGLE_APPLICATION_CREDENTIALS=/path/to/service-account-key.json
```

**Option B: Production (Workload Identity - GKE)**

```yaml
# No env var needed
# Setup workload identity di GKE:
apiVersion: v1
kind: ServiceAccount
metadata:
  name: audit-trail-sa
  annotations:
    iam.gke.io/gcp-service-account: audit-trail@PROJECT_ID.iam.gserviceaccount.com
```

---

## ▶️ Run Example

**Option 1: Using run.sh script (Recommended)**

```bash
cd examples
./run.sh
```

Script ini akan:
- ✅ Check .env file (create dari .env.example jika belum ada)
- ✅ Validate required environment variables
- ✅ Check service account key file (jika set)
- ✅ Display configuration
- ✅ Run service

**Option 2: Manual**

```bash
# Load environment variables
export $(cat examples/.env | grep -v '^#' | xargs)

# Run example service
go run examples/ex_service.go
```

Output:
```
🚀 Server starting on :8080
```

---

## 🧪 Testing API

**Quick Test (All Endpoints):**

```bash
# Di terminal lain (service harus jalan)
cd examples
./test_api.sh
```

Script ini akan test semua endpoints secara otomatis dan show hasilnya.

**Manual Testing:**

### 1. Health Check (No Audit)

```bash
curl http://localhost:8080/health
```

Response:
```json
{
  "status": "ok",
  "time": "2024-01-07T10:00:00Z"
}
```

### 2. Login (No Audit - Skipped)

```bash
curl -X POST http://localhost:8080/api/v1/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"secret"}'
```

Response:
```json
{
  "token": "Bearer valid-token-123",
  "user": {
    "id": "user-12345",
    "username": "admin"
  }
}
```

### 3. List Products (With Audit)

```bash
curl http://localhost:8080/api/v1/products \
  -H "Authorization: Bearer valid-token-123"
```

**Audit Record:**
```json
{
  "log_aduit_trail_id": "abc123...",
  "log_req_id": "req-1234567890",
  "log_action": "GET /api/v1/products",
  "log_endpoint": "/api/v1/products",
  "log_request": null,
  "log_response": null,
  "log_created_date": "2024-01-07T10:00:00Z",
  "log_created_by": "user-12345"
}
```

### 4. Create Product (With Audit + Request Body Capture)

```bash
curl -X POST http://localhost:8080/api/v1/products \
  -H "Authorization: Bearer valid-token-123" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Gaming Laptop",
    "price": 15000000,
    "stock": 10
  }'
```

**Audit Record:**
```json
{
  "log_aduit_trail_id": "def456...",
  "log_req_id": "req-1234567891",
  "log_action": "CREATE_PRODUCT",
  "log_endpoint": "/api/v1/products",
  "log_request": {
    "name": "Gaming Laptop",
    "price": 15000000,
    "stock": 10
  },
  "log_response": null,
  "log_created_date": "2024-01-07T10:01:00Z",
  "log_created_by": "user-12345"
}
```

### 5. Update Product

```bash
curl -X PUT http://localhost:8080/api/v1/products/prod-1 \
  -H "Authorization: Bearer valid-token-123" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Gaming Laptop Pro",
    "price": 18000000,
    "stock": 5
  }'
```

### 6. Delete Product

```bash
curl -X DELETE http://localhost:8080/api/v1/products/prod-1 \
  -H "Authorization: Bearer valid-token-123"
```

### 7. Create Order

```bash
curl -X POST http://localhost:8080/api/v1/orders \
  -H "Authorization: Bearer valid-token-123" \
  -H "Content-Type: application/json" \
  -d '{
    "product_id": "prod-1",
    "quantity": 2
  }'
```

### 8. Cancel Order

```bash
curl -X POST http://localhost:8080/api/v1/orders/order-123/cancel \
  -H "Authorization: Bearer valid-token-123" \
  -H "Content-Type: application/json" \
  -d '{
    "reason": "Customer changed mind"
  }'
```

---

## 🔍 Query Audit Logs

```sql
-- All audit logs today
SELECT * FROM audit_trail
WHERE log_created_date >= CURRENT_DATE
ORDER BY log_created_date DESC;

-- Logs by specific user
SELECT * FROM audit_trail
WHERE log_created_by = 'user-12345'
ORDER BY log_created_date DESC;

-- Logs for specific action
SELECT * FROM audit_trail
WHERE log_action = 'CREATE_PRODUCT'
ORDER BY log_created_date DESC;

-- Logs with request body (product creation)
SELECT
  log_aduit_trail_id,
  log_action,
  log_request::json->>'name' as product_name,
  log_request::json->>'price' as price,
  log_created_by,
  log_created_date
FROM audit_trail
WHERE log_action = 'CREATE_PRODUCT'
ORDER BY log_created_date DESC;

-- Count by action type
SELECT
  log_action,
  COUNT(*) as total
FROM audit_trail
GROUP BY log_action
ORDER BY total DESC;

-- Activity by user (last 24 hours)
SELECT
  log_created_by,
  COUNT(*) as total_actions,
  MIN(log_created_date) as first_action,
  MAX(log_created_date) as last_action
FROM audit_trail
WHERE log_created_date >= NOW() - INTERVAL '24 hours'
GROUP BY log_created_by
ORDER BY total_actions DESC;
```

---

## 🔧 Customization

### Custom Action Names

```go
func handleSomething(c *gin.Context) {
    // Set custom action name (lebih descriptive)
    c.Set("audit_action", "APPROVE_WITHDRAWAL")

    // Business logic...
    c.JSON(200, gin.H{"status": "approved"})
}
```

### Skip More Paths

```go
r.Use(audittrail.GinMiddleware(
    audittrail.WithSkipPaths(
        "/health",
        "/metrics",
        "/api/v1/login",
        "/api/v1/register",
        "/favicon.ico",
    ),
))
```

### Custom User Extractor

```go
r.Use(audittrail.GinMiddleware(
    audittrail.WithUserExtractor(func(c *gin.Context) string {
        // Custom logic untuk extract user
        if claims, exists := c.Get("jwt_claims"); exists {
            return claims.(JWTClaims).UserID
        }
        return "anonymous"
    }),
))
```

### Disable Request Body Capture

```go
r.Use(audittrail.GinMiddleware(
    audittrail.WithCaptureRequestBody(false), // Don't capture body
))
```

---

## 📊 Architecture Flow

```
┌─────────────┐
│   Client    │
└──────┬──────┘
       │ HTTP Request
       ▼
┌─────────────────────────────────────┐
│  Gin Middleware (audittrail)        │
│  1. Capture request body            │
│  2. Extract user_id from context    │
│  3. Process request handler         │
│  4. Build audit entry               │
│  5. Send to Pub/Sub (async)         │
└──────┬──────────────────────────────┘
       │
       ▼
┌─────────────────┐
│  GCP Pub/Sub    │
│  Topic:         │
│  audit-trail    │
└──────┬──────────┘
       │
       ▼
┌─────────────────────────────┐
│  Consumer (Background)      │
│  1. Listen from subscription│
│  2. Unmarshal entry         │
│  3. Save to Database        │
│  4. Ack/Nack message        │
└──────┬──────────────────────┘
       │
       ▼
┌─────────────────┐
│  PostgreSQL     │
│  audit_trail    │
│  table          │
└─────────────────┘
```

---

## 🔒 Production Checklist

- [ ] Environment variables di-set dengan benar
- [ ] GCP Pub/Sub topic dan subscription sudah dibuat
- [ ] Database table sudah dibuat
- [ ] Index database sudah dibuat untuk performance
- [ ] Service account permissions sudah benar
- [ ] Workload Identity configured (untuk GKE)
- [ ] Monitoring setup (Pub/Sub metrics, database metrics)
- [ ] Log retention policy configured
- [ ] Backup strategy untuk audit logs
- [ ] Security: Jangan audit sensitive data (password, credit card, etc)

---

## 🐛 Troubleshooting

### Error: "not initialized, call InitFromEnv first"

**Solusi:**
```go
// Pastikan InitFromEnv() dipanggil SEBELUM setup middleware
if err := audittrail.InitFromEnv(ctx); err != nil {
    log.Fatal(err)
}
```

### Error: "Failed to publish to pubsub"

**Solusi:**
1. Check GCP credentials: `echo $GOOGLE_APPLICATION_CREDENTIALS`
2. Verify topic exists: `gcloud pubsub topics list`
3. Check service account permissions

### Error: "connection refused" ke database

**Solusi:**
1. Check database is running: `pg_isready -h localhost`
2. Verify DSN connection string
3. Check firewall/network rules

### Consumer tidak consume message

**Solusi:**
1. Check subscription exists: `gcloud pubsub subscriptions list`
2. Verify subscription attached to correct topic
3. Check consumer logs untuk error messages

---

## 📝 Notes

- Request body capture hanya untuk `POST`, `PUT`, `PATCH`
- Max body size default: 1MB (bisa di-adjust)
- Audit dilakukan async (non-blocking) untuk performa
- Error di audit trail TIDAK akan stop request handling
- Message di-retry otomatis oleh Pub/Sub jika gagal

---

## 📚 References

- [Audit Trail Package](https://github.com/ahsansandiah/audit-trail)
- [GCP Pub/Sub Documentation](https://cloud.google.com/pubsub/docs)
- [Gin Framework](https://gin-gonic.com/)
