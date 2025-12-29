package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"github.com/ahsansandiah/audit-trail"
)

// memoryDriver is a tiny SQL driver that only logs executed queries.
type memoryDriver struct{}

func (d *memoryDriver) Open(_ string) (driver.Conn, error) {
	return &memoryConn{}, nil
}

type memoryConn struct{}

func (c *memoryConn) Prepare(_ string) (driver.Stmt, error) {
	return nil, fmt.Errorf("not implemented")
}
func (c *memoryConn) Close() error              { return nil }
func (c *memoryConn) Begin() (driver.Tx, error) { return nil, fmt.Errorf("not implemented") }

// Exec returns ErrSkip so database/sql uses ExecContext.
func (c *memoryConn) Exec(_ string, _ []driver.Value) (driver.Result, error) {
	return nil, driver.ErrSkip
}

// ExecContext logs query and arguments.
func (c *memoryConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	log.Printf("[memoryDriver] EXEC: %s", strings.TrimSpace(query))
	if len(args) > 0 {
		printArgs := make([]any, len(args))
		for i, a := range args {
			printArgs[i] = a.Value
		}
		log.Printf("[memoryDriver] ARGS: %+v", printArgs)
	}
	return driver.RowsAffected(1), nil
}

type memoryPubSub struct {
	ch chan audittrail.Entry
}

func (m *memoryPubSub) Publish(ctx context.Context, entry audittrail.Entry) error {
	select {
	case m.ch <- entry:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *memoryPubSub) Receive(ctx context.Context, handler func(context.Context, audittrail.Entry) error) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case entry := <-m.ch:
			if err := handler(ctx, entry); err != nil {
				return err
			}
		}
	}
}

// Example showing how to wrap HTTP handlers with the audit trail middleware.
func main() {
	const driverName = "audittrail_memory_http"
	sql.Register(driverName, &memoryDriver{})

	db, err := sql.Open(driverName, "")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	audit, err := audittrail.NewAuditTrail(audittrail.Config{DB: db})
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	if err := audit.EnsureTable(ctx); err != nil {
		log.Fatal(err)
	}

	pubsub := &memoryPubSub{ch: make(chan audittrail.Entry, 16)}
	recorder, err := audittrail.NewPubSubRecorder(pubsub, nil)
	if err != nil {
		log.Fatal(err)
	}

	consumer, err := audittrail.NewConsumer(audit, pubsub, nil)
	if err != nil {
		log.Fatal(err)
	}

	consumeCtx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := consumer.Run(consumeCtx); err != nil && err != context.Canceled {
			log.Printf("consumer stopped: %v", err)
		}
	}()

	// Wrap handler with middleware.
	handler := audittrail.HTTPMiddleware(
		recorder,
		audittrail.WithActorHeader("X-User-Id"),
		audittrail.WithRequestIDHeader("X-Request-Id"),
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("ok"))
	}))

	// Simulate a request using httptest.
	req := httptest.NewRequest(http.MethodPost, "/api/orders", nil)

	req.Header.Set("X-Request-Id", "req-001")
	req.Header.Set("X-User-Id", "user-123")
	req.Header.Set("X-Forwarded-For", "10.0.0.5")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	log.Printf("response status: %d", rr.Code)
	log.Println("middleware example finished")

	time.Sleep(100 * time.Millisecond) // allow logs to flush before exit
	cancel()
	wg.Wait()
}
