package audittrail

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"os"
	"strings"
	"sync"

	"cloud.google.com/go/pubsub"
)

const (
	// Hardcoded configurations
	dbDriver           = "pgx"
	auditTable         = "log_audit_trail"
	loginActivityTable = "log_user_login_activity"
	pubsubTopic        = "audit-trail"
	pubsubSubscription = "audit-trail-sub"

	// Environment variable keys for GCP Project
	envGCPProject = "AUDIT_GCP_PROJECT"

	// Environment variable keys for database connection (standard naming)
	envDBUser     = "DATABASE_USER"
	envDBPassword = "DATABASE_PASSWORD"
	envDBHost     = "DATABASE_HOST"
	envDBPort     = "DATABASE_PORT"
	envDBName     = "DATABASE_NAME"
)

// InitOptions configures audit trail initialization with custom handlers
type InitOptions struct {
	// OnConsumerError is called when consumer fails to process a message (e.g., DB insert error)
	OnConsumerError func(err error)

	// OnPublishError is called when publishing to Pub/Sub fails
	OnPublishError func(err error)

	// SecretProvider for fetching config from GCP Secret Manager (optional)
	SecretProvider SecretProvider
}

var runtime struct {
	mu                    sync.Mutex
	initialized           bool
	initializing          bool
	recorder              Recorder
	loginActivityRecorder *UserLoginActivityRecorder
	cancel                context.CancelFunc
	wg                    sync.WaitGroup
	db                    *sql.DB
	pubsub                *pubsub.Client
	options               *InitOptions
}

// InitFromEnv initializes a global recorder and consumer using GCP Pub/Sub + DB.
// Configuration is loaded from environment variables.
// It is safe to call multiple times; only the first call will initialize.
func InitFromEnv(ctx context.Context) error {
	return InitWithOptions(ctx, nil)
}

// InitFromEnvOrSecrets initializes using environment variables with optional secret provider fallback.
// If provider is nil, behaves like InitFromEnv (environment variables only).
// If provider is set, tries environment variables first, then falls back to secret provider.
// It is safe to call multiple times; only the first call will initialize.
func InitFromEnvOrSecrets(ctx context.Context, provider SecretProvider) error {
	return InitWithOptions(ctx, &InitOptions{SecretProvider: provider})
}

// InitWithOptions initializes audit trail with custom options including error handlers.
// Use this to capture errors for monitoring, alerting, or custom logging.
//
// Example:
//
//	audittrail.InitWithOptions(ctx, &audittrail.InitOptions{
//	    OnConsumerError: func(err error) {
//	        sentry.CaptureException(err)
//	        log.Printf("audit consumer error: %v", err)
//	    },
//	    OnPublishError: func(err error) {
//	        metrics.AuditPublishErrors.Inc()
//	    },
//	})
func InitWithOptions(ctx context.Context, opts *InitOptions) error {
	if opts == nil {
		opts = &InitOptions{}
	}
	provider := opts.SecretProvider
	runtime.mu.Lock()
	if runtime.initialized {
		runtime.mu.Unlock()
		return nil
	}
	if runtime.initializing {
		runtime.mu.Unlock()
		return errors.New("audittrail: initialization already in progress")
	}
	runtime.initializing = true
	runtime.mu.Unlock()
	ok := false
	defer func() {
		if ok {
			return
		}
		runtime.mu.Lock()
		runtime.initializing = false
		runtime.mu.Unlock()
	}()

	// Helper to get config from env var or secret provider
	getConfig := func(envKey, secretKey, defaultVal string) string {
		return getEnvOrSecret(ctx, provider, envKey, secretKey, defaultVal)
	}

	// GCP Project ID from env
	projectID := getConfig(envGCPProject, "audit-gcp-project", "local-project")

	// Build database DSN from standard env vars
	dbUser := getConfig(envDBUser, "database-user", "postgres")
	dbPassword := getConfig(envDBPassword, "database-password", "postgres")
	dbHost := getConfig(envDBHost, "database-host", "localhost")
	dbPort := getConfig(envDBPort, "database-port", "5432")
	dbName := getConfig(envDBName, "database-name", "audittrail")
	dbDSN := buildPostgresDSN(dbUser, dbPassword, dbHost, dbPort, dbName)

	db, err := sql.Open(dbDriver, dbDSN)
	if err != nil {
		return err
	}

	// Use PostgreSQL placeholder style (hardcoded driver is pgx)
	placeholderStyle := PlaceholderDollar

	audit, err := NewAuditTrail(Config{
		DB:          db,
		TableName:   auditTable,
		Placeholder: placeholderStyle,
	})
	if err != nil {
		_ = db.Close()
		return err
	}

	loginActivityRec, err := NewUserLoginActivityRecorder(UserLoginActivityConfig{
		DB:          db,
		TableName:   loginActivityTable,
		Placeholder: placeholderStyle,
	})
	if err != nil {
		_ = db.Close()
		return err
	}

	client, err := pubsub.NewClient(ctx, projectID)
	if err != nil {
		_ = db.Close()
		return err
	}

	recorder, err := NewPubSubRecorder(NewGCPPublisher(client.Topic(pubsubTopic)), nil)
	if err != nil {
		_ = client.Close()
		_ = db.Close()
		return err
	}

	// Use custom error handler if provided, otherwise use default logger
	consumerErrorHandler := opts.OnConsumerError
	if consumerErrorHandler == nil {
		consumerErrorHandler = func(err error) {
			log.Printf("audittrail consumer error: %v", err)
		}
	}

	consumer, err := NewConsumer(audit, NewGCPSubscriber(client.Subscription(pubsubSubscription)), consumerErrorHandler)
	if err != nil {
		_ = client.Close()
		_ = db.Close()
		return err
	}

	runCtx, cancel := context.WithCancel(ctx)
	runtime.wg.Add(1)
	go func() {
		defer runtime.wg.Done()
		if err := consumer.Run(runCtx); err != nil && !errors.Is(err, context.Canceled) {
			if opts.OnConsumerError != nil {
				opts.OnConsumerError(err)
			} else {
				log.Printf("audittrail consumer stopped: %v", err)
			}
		}
	}()

	runtime.mu.Lock()
	runtime.initialized = true
	runtime.initializing = false
	runtime.recorder = recorder
	runtime.loginActivityRecorder = loginActivityRec
	runtime.cancel = cancel
	runtime.db = db
	runtime.pubsub = client
	runtime.options = opts
	runtime.mu.Unlock()

	ok = true
	return nil
}

// Record publishes an audit entry using the default recorder.
func Record(ctx context.Context, entry Entry) error {
	runtime.mu.Lock()
	recorder := runtime.recorder
	opts := runtime.options
	runtime.mu.Unlock()
	if recorder == nil {
		return errors.New("audittrail: not initialized, call InitFromEnv first")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	err := recorder.Record(ctx, entry)
	if err != nil && opts != nil && opts.OnPublishError != nil {
		opts.OnPublishError(err)
	}
	return err
}

// Shutdown stops the consumer and closes resources initialized by InitFromEnv.
func Shutdown(ctx context.Context) error {
	runtime.mu.Lock()
	if !runtime.initialized {
		runtime.initializing = false
		runtime.mu.Unlock()
		return nil
	}
	cancel := runtime.cancel
	db := runtime.db
	client := runtime.pubsub
	runtime.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	done := make(chan struct{})
	go func() {
		runtime.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	}

	if client != nil {
		_ = client.Close()
	}
	if db != nil {
		_ = db.Close()
	}
	runtime.mu.Lock()
	runtime.initialized = false
	runtime.initializing = false
	runtime.recorder = nil
	runtime.loginActivityRecorder = nil
	runtime.cancel = nil
	runtime.db = nil
	runtime.pubsub = nil
	runtime.mu.Unlock()
	return nil
}

// RecordLogin records a user login activity and returns the generated activity ID.
// The returned ID can be used to link audit trail entries to this login session.
func RecordLogin(ctx context.Context, activity UserLoginActivity) (string, error) {
	runtime.mu.Lock()
	recorder := runtime.loginActivityRecorder
	runtime.mu.Unlock()
	if recorder == nil {
		return "", errors.New("audittrail: not initialized, call InitFromEnv first")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return recorder.RecordLogin(ctx, activity)
}

// RecordLogout updates the logout time for a login activity.
func RecordLogout(ctx context.Context, activityID string) error {
	runtime.mu.Lock()
	recorder := runtime.loginActivityRecorder
	runtime.mu.Unlock()
	if recorder == nil {
		return errors.New("audittrail: not initialized, call InitFromEnv first")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return recorder.RecordLogout(ctx, activityID)
}

// GetLoginActivity retrieves a login activity by ID.
func GetLoginActivity(ctx context.Context, activityID string) (*UserLoginActivity, error) {
	runtime.mu.Lock()
	recorder := runtime.loginActivityRecorder
	runtime.mu.Unlock()
	if recorder == nil {
		return nil, errors.New("audittrail: not initialized, call InitFromEnv first")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return recorder.GetByID(ctx, activityID)
}

// GetLoginActivityByToken retrieves an active login activity by session token.
func GetLoginActivityByToken(ctx context.Context, sessionToken string) (*UserLoginActivity, error) {
	runtime.mu.Lock()
	recorder := runtime.loginActivityRecorder
	runtime.mu.Unlock()
	if recorder == nil {
		return nil, errors.New("audittrail: not initialized, call InitFromEnv first")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return recorder.GetBySessionToken(ctx, sessionToken)
}

// GetLoginActivityRecorder returns the global UserLoginActivityRecorder instance.
// Returns nil if not initialized.
func GetLoginActivityRecorder() *UserLoginActivityRecorder {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return runtime.loginActivityRecorder
}

// buildPostgresDSN constructs a PostgreSQL connection string from components
func buildPostgresDSN(user, password, host, port, dbname string) string {
	return "postgres://" + user + ":" + password + "@" + host + ":" + port + "/" + dbname + "?sslmode=disable"
}

// getEnvOrSecret tries to get config from environment variable first,
// then falls back to secret provider if available
func getEnvOrSecret(ctx context.Context, provider SecretProvider, envKey, secretKey, defaultVal string) string {
	// 1. Try environment variable first
	if val := strings.TrimSpace(os.Getenv(envKey)); val != "" {
		return val
	}

	// 2. Try secret provider if available
	if provider != nil {
		if val, err := provider.GetSecret(ctx, secretKey); err == nil && strings.TrimSpace(val) != "" {
			return strings.TrimSpace(val)
		}
		// Don't log error here to avoid noise for optional secrets
	}

	// 3. Use default
	return defaultVal
}
