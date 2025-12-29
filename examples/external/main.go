package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/ahsansandiah/audit-trail"
)

// fakePubSub simulates an external queue (e.g., Pub/Sub).
type fakePubSub struct {
	ch chan audittrail.Entry
}

func (p *fakePubSub) Publish(ctx context.Context, entry audittrail.Entry) error {
	payload, _ := json.Marshal(entry)
	log.Printf("[fake-pubsub] publish: %s", payload)
	select {
	case p.ch <- entry:
		time.Sleep(20 * time.Millisecond) // simulate network latency
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *fakePubSub) Receive(ctx context.Context, handler func(context.Context, audittrail.Entry) error) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case entry := <-p.ch:
			if err := handler(ctx, entry); err != nil {
				return err
			}
		}
	}
}

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

func main() {
	pubsub := &fakePubSub{ch: make(chan audittrail.Entry, 32)}

	const driverName = "audittrail_memory_external"
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

	if err := audit.EnsureTable(context.Background()); err != nil {
		log.Fatal(err)
	}

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

	// Publish events; consumer will store them in DB.
	for i := 0; i < 3; i++ {
		entry := audittrail.Entry{
			Action:    "user-login",
			Actor:     "user",
			RequestID: fmt.Sprintf("req-%03d", i),
		}
		if err := recorder.Record(context.Background(), entry); err != nil {
			log.Printf("publish failed: %v", err)
		}
	}

	// Wait briefly to let background worker flush before exit.
	time.Sleep(100 * time.Millisecond)
	cancel()
	wg.Wait()
	log.Println("external pubsub example finished")
}
