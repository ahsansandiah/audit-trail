# Run example service with environment variables (Windows PowerShell)

Write-Host "=== Audit Trail Example Service ===" -ForegroundColor Green
Write-Host ""

# Check if .env exists
if (-not (Test-Path ".env")) {
    Write-Host "Warning: .env file not found" -ForegroundColor Yellow
    Write-Host "Creating .env from .env.example..."

    if (Test-Path ".env.example") {
        Copy-Item ".env.example" ".env"
        Write-Host "Done! .env created!" -ForegroundColor Green
        Write-Host "Please edit .env with your actual credentials" -ForegroundColor Yellow
        Write-Host ""
        Read-Host "Press Enter to continue or Ctrl+C to exit and edit .env first"
    } else {
        Write-Host "Error: .env.example not found" -ForegroundColor Red
        exit 1
    }
}

# Load .env
Write-Host "Loading environment variables..." -ForegroundColor Green
Get-Content ".env" | ForEach-Object {
    if ($_ -match "^\s*([^#][^=]+)=(.*)$") {
        $name = $matches[1].Trim()
        $value = $matches[2].Trim()
        [Environment]::SetEnvironmentVariable($name, $value, "Process")
    }
}

# Check required env vars
$requiredVars = @("AUDIT_GCP_PROJECT", "AUDIT_PUBSUB_TOPIC", "AUDIT_PUBSUB_SUBSCRIPTION", "AUDIT_DB_DSN")
$missingVars = @()

foreach ($var in $requiredVars) {
    if (-not [Environment]::GetEnvironmentVariable($var)) {
        $missingVars += $var
    }
}

if ($missingVars.Count -gt 0) {
    Write-Host "Error: Missing required environment variables:" -ForegroundColor Red
    foreach ($var in $missingVars) {
        Write-Host "   - $var"
    }
    Write-Host ""
    Write-Host "Please edit .env file and set these variables" -ForegroundColor Yellow
    exit 1
}

Write-Host "Done! Environment variables loaded" -ForegroundColor Green
Write-Host ""

# Display config
Write-Host "Configuration:" -ForegroundColor Green
Write-Host "  GCP Project: $env:AUDIT_GCP_PROJECT"
Write-Host "  Pub/Sub Topic: $env:AUDIT_PUBSUB_TOPIC"
Write-Host "  Pub/Sub Subscription: $env:AUDIT_PUBSUB_SUBSCRIPTION"
$dbDriver = if ($env:AUDIT_DB_DRIVER) { $env:AUDIT_DB_DRIVER } else { "pgx" }
$dbTable = if ($env:AUDIT_TABLE) { $env:AUDIT_TABLE } else { "audit_trail" }
Write-Host "  Database Driver: $dbDriver"
Write-Host "  Database Table: $dbTable"
Write-Host ""

# Check GCP authentication mode
if ($env:PUBSUB_EMULATOR_HOST) {
    # Emulator mode: no credentials needed
    Write-Host "Done! Auth: Pub/Sub Emulator mode" -ForegroundColor Green
    Write-Host "   Emulator host: $env:PUBSUB_EMULATOR_HOST"
    Write-Host "   (No credentials required)"
    Write-Host ""
} elseif ($env:GOOGLE_APPLICATION_CREDENTIALS) {
    # Local mode: using service account key file
    if (-not (Test-Path $env:GOOGLE_APPLICATION_CREDENTIALS)) {
        Write-Host "Error: Service account key file not found: $env:GOOGLE_APPLICATION_CREDENTIALS" -ForegroundColor Red
        exit 1
    }
    Write-Host "Done! Auth: Service account key file" -ForegroundColor Green
    Write-Host ""
} else {
    # Production mode or gcloud CLI auth
    Write-Host "Done! Auth: Application Default Credentials (ADC)" -ForegroundColor Green
    Write-Host "   (Using gcloud CLI login or GCP attached service account)"
    Write-Host ""
}

# Run service
Write-Host "Starting service on :8080..." -ForegroundColor Green
Write-Host ""
go run ex_service.go
