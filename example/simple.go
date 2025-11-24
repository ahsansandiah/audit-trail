package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"log"
	"strings"
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

func main() {
	const driverName = "audittrail_memory"
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

	entry := audittrail.Entry{
		RequestID: "req-001",
		Actor:     "user-123",
		Action:    "create-order",
		Endpoint:  "/api/orders",
		Request:   map[string]any{"item_id": 42},
		Response:  map[string]any{"status": "ok"},
		IPAddress: "10.0.0.1",
		CreatedAt: time.Now().UTC(),
		CreatedBy: "service-a",
	}

	if err := audit.Record(ctx, entry); err != nil {
		log.Fatal(err)
	}

	log.Println("example finished")
}
