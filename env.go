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
	defaultGCPProject      = "local-project"
	defaultPubSubTopic     = "audit-trail"
	defaultPubSubSub       = "audit-trail-sub"
	defaultDBDriver        = "pgx"
	defaultDBDSN           = "postgres://user:pass@localhost:5432/audittrail?sslmode=disable"
	defaultAuditTable      = "audit_trail"
	envGCPProject          = "AUDIT_GCP_PROJECT"
	envPubSubTopic         = "AUDIT_PUBSUB_TOPIC"
	envPubSubSubscription  = "AUDIT_PUBSUB_SUBSCRIPTION"
	envDBDriver            = "AUDIT_DB_DRIVER"
	envDBDSN               = "AUDIT_DB_DSN"
	envAuditTable          = "AUDIT_TABLE"
)

var runtime struct {
	mu          sync.Mutex
	initialized bool
	initializing bool
	recorder    Recorder
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	db          *sql.DB
	pubsub      *pubsub.Client
}

// InitFromEnv initializes a global recorder and consumer using GCP Pub/Sub + DB.
// It is safe to call multiple times; only the first call will initialize.
func InitFromEnv(ctx context.Context) error {
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

	projectID := getenv(envGCPProject, defaultGCPProject)
	topicName := getenv(envPubSubTopic, defaultPubSubTopic)
	subscriptionName := getenv(envPubSubSubscription, defaultPubSubSub)
	dbDriver := getenv(envDBDriver, defaultDBDriver)
	dbDSN := getenv(envDBDSN, defaultDBDSN)
	table := getenv(envAuditTable, defaultAuditTable)

	db, err := sql.Open(dbDriver, dbDSN)
	if err != nil {
		return err
	}

	audit, err := NewAuditTrail(Config{
		DB:        db,
		TableName: table,
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

	recorder, err := NewPubSubRecorder(&gcpPublisher{topic: client.Topic(topicName)}, nil)
	if err != nil {
		_ = client.Close()
		_ = db.Close()
		return err
	}

	consumer, err := NewConsumer(audit, &gcpSubscriber{sub: client.Subscription(subscriptionName)}, nil)
	if err != nil {
		_ = client.Close()
		_ = db.Close()
		return err
	}

	runCtx, cancel := context.WithCancel(context.Background())
	runtime.wg.Add(1)
	go func() {
		defer runtime.wg.Done()
		if err := consumer.Run(runCtx); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("audittrail consumer stopped: %v", err)
		}
	}()

	runtime.mu.Lock()
	runtime.initialized = true
	runtime.initializing = false
	runtime.recorder = recorder
	runtime.cancel = cancel
	runtime.db = db
	runtime.pubsub = client
	runtime.mu.Unlock()

	ok = true
	return nil
}

// Record publishes an audit entry using the default recorder.
func Record(ctx context.Context, entry Entry) error {
	runtime.mu.Lock()
	recorder := runtime.recorder
	runtime.mu.Unlock()
	if recorder == nil {
		return errors.New("audittrail: not initialized, call InitFromEnv first")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return recorder.Record(ctx, entry)
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
	runtime.cancel = nil
	runtime.db = nil
	runtime.pubsub = nil
	runtime.mu.Unlock()
	return nil
}

func getenv(key, def string) string {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return def
	}
	return val
}
