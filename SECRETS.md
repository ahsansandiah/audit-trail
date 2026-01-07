# Cloud Secrets Support

Audit Trail library mendukung loading konfigurasi dari:
1. **Environment Variables** (default)
2. **GCP Secret Manager**
3. **AWS Secrets Manager** (coming soon)
4. **Custom Secret Providers**

## Quick Start

### Option 1: Environment Variables (Default)

```go
// Menggunakan environment variables saja (backward compatible)
audittrail.InitFromEnv(ctx)
```

Environment variables yang digunakan:
- `AUDIT_GCP_PROJECT`
- `AUDIT_PUBSUB_TOPIC`
- `AUDIT_PUBSUB_SUBSCRIPTION`
- `AUDIT_DB_DRIVER`
- `AUDIT_DB_DSN`
- `AUDIT_TABLE`

### Option 2: GCP Secret Manager

```go
import (
    "context"
    "github.com/ahsansandiah/audit-trail"
)

func main() {
    ctx := context.Background()

    // Create GCP Secret Manager provider
    provider, err := audittrail.NewGCPSecretProvider(ctx, "my-gcp-project")
    if err != nil {
        log.Fatal(err)
    }
    defer provider.Close()

    // Initialize dengan secret provider
    // Akan coba env var dulu, jika tidak ada baru dari Secret Manager
    if err := audittrail.InitFromEnvOrSecrets(ctx, provider); err != nil {
        log.Fatal(err)
    }
    defer audittrail.Shutdown(ctx)

    // ... rest of your app
}
```

### Option 3: Hybrid (Env Vars + Secrets)

```go
// Service akan:
// 1. Coba baca dari environment variable dulu
// 2. Jika tidak ada, baca dari GCP Secret Manager
// 3. Jika tidak ada, gunakan default value

provider, _ := audittrail.NewGCPSecretProvider(ctx, "my-project")
audittrail.InitFromEnvOrSecrets(ctx, provider)
```

**Priority:**
```
Environment Variable → Secret Manager → Default Value
```

## GCP Secret Manager Setup

### 1. Create Secrets di GCP

```bash
# Create secrets
gcloud secrets create audit-db-dsn --data-file=- <<EOF
postgres://user:pass@localhost:5432/audit?sslmode=disable
EOF

gcloud secrets create audit-gcp-project --data-file=- <<EOF
my-gcp-project
EOF

gcloud secrets create audit-pubsub-topic --data-file=- <<EOF
audit-trail
EOF

gcloud secrets create audit-pubsub-subscription --data-file=- <<EOF
audit-trail-sub
EOF
```

### 2. Grant Access ke Service Account

```bash
# Grant access to service account
gcloud secrets add-iam-policy-binding audit-db-dsn \
    --member="serviceAccount:my-service@my-project.iam.gserviceaccount.com" \
    --role="roles/secretmanager.secretAccessor"

# Repeat for other secrets
```

### 3. Use in Application

```go
provider, err := audittrail.NewGCPSecretProvider(ctx, "my-gcp-project")
if err != nil {
    log.Fatal(err)
}
defer provider.Close()

audittrail.InitFromEnvOrSecrets(ctx, provider)
```

## Secret Naming Convention

Library menggunakan naming convention berikut untuk secrets:

| Environment Variable | Secret Name | Description |
|---------------------|-------------|-------------|
| `AUDIT_GCP_PROJECT` | `audit-gcp-project` | GCP Project ID |
| `AUDIT_PUBSUB_TOPIC` | `audit-pubsub-topic` | Pub/Sub topic name |
| `AUDIT_PUBSUB_SUBSCRIPTION` | `audit-pubsub-subscription` | Pub/Sub subscription |
| `AUDIT_DB_DRIVER` | `audit-db-driver` | Database driver (pgx, mysql, etc) |
| `AUDIT_DB_DSN` | `audit-db-dsn` | Database connection string |
| `AUDIT_TABLE` | `audit-table` | Audit table name |

## Kubernetes Integration

### Option A: Mount Secrets as Env Vars (Recommended)

```yaml
apiVersion: v1
kind: Deployment
spec:
  template:
    spec:
      containers:
      - name: app
        env:
          - name: AUDIT_DB_DSN
            valueFrom:
              secretKeyRef:
                name: audit-secrets
                key: db-dsn
```

**Code:**
```go
// Tidak perlu secret provider, K8s sudah inject sebagai env var
audittrail.InitFromEnv(ctx)
```

### Option B: Use GCP Workload Identity + Secret Manager

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: audit-sa
  annotations:
    iam.gke.io/gcp-service-account: my-service@my-project.iam.gserviceaccount.com

---
apiVersion: v1
kind: Deployment
spec:
  template:
    spec:
      serviceAccountName: audit-sa
      containers:
      - name: app
        env:
          - name: GCP_PROJECT_ID
            value: "my-project"
```

**Code:**
```go
projectID := os.Getenv("GCP_PROJECT_ID")
provider, _ := audittrail.NewGCPSecretProvider(ctx, projectID)
audittrail.InitFromEnvOrSecrets(ctx, provider)
```

## Custom Secret Provider

Implement interface `SecretProvider` untuk custom provider:

```go
type SecretProvider interface {
    GetSecret(ctx context.Context, key string) (string, error)
}
```

### Example: HashiCorp Vault

```go
type VaultSecretProvider struct {
    client *vault.Client
    path   string
}

func (p *VaultSecretProvider) GetSecret(ctx context.Context, key string) (string, error) {
    secret, err := p.client.Logical().Read(p.path + "/" + key)
    if err != nil {
        return "", err
    }

    if val, ok := secret.Data["value"].(string); ok {
        return val, nil
    }

    return "", fmt.Errorf("secret not found")
}

// Usage
vaultProvider := &VaultSecretProvider{client: vaultClient, path: "secret/audit"}
audittrail.InitFromEnvOrSecrets(ctx, vaultProvider)
```

### Example: AWS Secrets Manager (Placeholder)

```go
// Will be implemented in future version
awsProvider, _ := audittrail.NewAWSSecretProvider("us-west-2")
audittrail.InitFromEnvOrSecrets(ctx, awsProvider)
```

## Testing with Mock Secrets

```go
// Use MapSecretProvider for testing
func TestWithSecrets(t *testing.T) {
    provider := audittrail.NewMapSecretProvider(map[string]string{
        "audit-db-dsn": "postgres://test@localhost/test",
        "audit-gcp-project": "test-project",
    })

    err := audittrail.InitFromEnvOrSecrets(context.Background(), provider)
    if err != nil {
        t.Fatal(err)
    }
}
```

## Security Best Practices

### ✅ DO:
- Use Secret Manager untuk production credentials
- Rotate secrets regularly
- Grant minimal IAM permissions (secretAccessor only)
- Use Workload Identity di GKE
- Use separate secrets per environment (dev/staging/prod)

### ❌ DON'T:
- Commit secrets ke git
- Use plain text env vars untuk production
- Share secrets across environments
- Grant overly broad IAM permissions

## Performance Considerations

### Caching
Library **tidak cache** secrets. Secrets di-load sekali saat `InitFromEnvOrSecrets()`.

### Latency
- Environment variables: **instant**
- GCP Secret Manager: **~50-100ms** (network call)
- AWS Secrets Manager: **~50-100ms** (network call)

**Recommendation:** Gunakan environment variables untuk development, Secret Manager untuk production.

## Migration Guide

### From env vars only → Hybrid

**Before:**
```go
audittrail.InitFromEnv(ctx)
```

**After:**
```go
provider, _ := audittrail.NewGCPSecretProvider(ctx, "my-project")
defer provider.Close()

audittrail.InitFromEnvOrSecrets(ctx, provider)
// Backward compatible: env vars masih bekerja!
```

**Rollout Strategy:**
1. Deploy code baru dengan `InitFromEnvOrSecrets(ctx, nil)` - sama seperti `InitFromEnv`
2. Create secrets di GCP Secret Manager
3. Deploy dengan provider: `InitFromEnvOrSecrets(ctx, provider)`
4. Remove env vars dari deployment

## Troubleshooting

### Error: "failed to create secret manager client"
**Cause:** GCP credentials tidak ter-configure.

**Solution:**
```bash
# Local development
gcloud auth application-default login

# Production (GKE)
# Use Workload Identity atau set GOOGLE_APPLICATION_CREDENTIALS
```

### Error: "failed to access secret"
**Cause:** Secret tidak ada atau permission tidak cukup.

**Solution:**
```bash
# Check secret exists
gcloud secrets describe audit-db-dsn

# Grant access
gcloud secrets add-iam-policy-binding audit-db-dsn \
    --member="serviceAccount:YOUR_SA@PROJECT.iam.gserviceaccount.com" \
    --role="roles/secretmanager.secretAccessor"
```

### Secrets tidak ter-load
**Debug:**
```go
provider, _ := audittrail.NewGCPSecretProvider(ctx, "my-project")

// Test manually
val, err := provider.GetSecret(ctx, "audit-db-dsn")
if err != nil {
    log.Printf("Failed to get secret: %v", err)
}
log.Printf("Secret value: %s", val)
```

## Examples

Lihat contoh lengkap di:
- `examples/gcp-secrets/` - GCP Secret Manager example
- `examples/gin-example/` - Gin dengan secrets support
