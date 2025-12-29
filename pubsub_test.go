package audittrail

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"testing"
	"time"
)

func TestPubSubRecorderPublishesNormalizedEntry(t *testing.T) {
	var got Entry
	pub := PublisherFunc(func(ctx context.Context, entry Entry) error {
		got = entry
		return nil
	})

	recorder, err := NewPubSubRecorder(pub, func() time.Time {
		return time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	})
	if err != nil {
		t.Fatalf("NewPubSubRecorder: %v", err)
	}

	if err := recorder.Record(context.Background(), Entry{Action: "login"}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	if got.ID == "" {
		t.Fatalf("expected ID to be set")
	}
	if got.CreatedAt.IsZero() {
		t.Fatalf("expected CreatedAt to be set")
	}
	if got.Action != "login" {
		t.Fatalf("expected Action to be preserved, got %q", got.Action)
	}
}

func TestConsumerPersistsEntry(t *testing.T) {
	var calls []execCall

	driverName := fmt.Sprintf("audittrail_stub_consumer_%d", time.Now().UnixNano())
	sql.Register(driverName, &stubDriver{
		execFn: func(query string, args []driver.NamedValue) (driver.Result, error) {
			calls = append(calls, execCall{query: query, args: args})
			return stubResult{}, nil
		},
	})

	db, err := sql.Open(driverName, "")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()

	audit, err := NewAuditTrail(Config{DB: db, Placeholder: PlaceholderQuestion})
	if err != nil {
		t.Fatalf("NewAuditTrail: %v", err)
	}

	sub := SubscriberFunc(func(ctx context.Context, handler func(context.Context, Entry) error) error {
		return handler(ctx, Entry{Action: "consume-test"})
	})

	consumer, err := NewConsumer(audit, sub, nil)
	if err != nil {
		t.Fatalf("NewConsumer: %v", err)
	}

	if err := consumer.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(calls) != 1 {
		t.Fatalf("expected 1 DB call, got %d", len(calls))
	}
}
