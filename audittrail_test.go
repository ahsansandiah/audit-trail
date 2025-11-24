package audittrail

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

type execCall struct {
	query string
	args  []driver.NamedValue
}

type stubDriver struct {
	execFn func(query string, args []driver.NamedValue) (driver.Result, error)
}

func (d *stubDriver) Open(_ string) (driver.Conn, error) {
	return &stubConn{execFn: d.execFn}, nil
}

type stubConn struct {
	execFn func(query string, args []driver.NamedValue) (driver.Result, error)
}

func (c *stubConn) Prepare(_ string) (driver.Stmt, error) { return nil, errors.New("not implemented") }
func (c *stubConn) Close() error                          { return nil }
func (c *stubConn) Begin() (driver.Tx, error)             { return nil, errors.New("not implemented") }

// ExecContext captures query execution without using Prepare.
func (c *stubConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if c.execFn == nil {
		return nil, errors.New("execFn missing")
	}
	return c.execFn(query, args)
}

// Exec returns ErrSkip so database/sql uses ExecContext.
func (c *stubConn) Exec(_ string, _ []driver.Value) (driver.Result, error) {
	return nil, driver.ErrSkip
}

type stubResult struct{}

func (stubResult) LastInsertId() (int64, error) { return 0, nil }
func (stubResult) RowsAffected() (int64, error) { return 1, nil }

func TestRecordInsertsData(t *testing.T) {
	var calls []execCall

	driverName := fmt.Sprintf("audittrail_stub_%d", time.Now().UnixNano())
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

	rec, err := NewAuditTrail(Config{DB: db, Placeholder: PlaceholderQuestion})
	if err != nil {
		t.Fatalf("NewAuditTrail: %v", err)
	}

	err = rec.Record(context.Background(), Entry{Action: "test"})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}

	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if !strings.Contains(calls[0].query, "INSERT INTO audit_trail") {
		t.Fatalf("unexpected query: %s", calls[0].query)
	}
	if len(calls[0].args) != 10 {
		t.Fatalf("expected 10 args, got %d", len(calls[0].args))
	}
}

func TestInvalidTableName(t *testing.T) {
	driverName := fmt.Sprintf("audittrail_stub_%d", time.Now().UnixNano())
	sql.Register(driverName, &stubDriver{})
	db, err := sql.Open(driverName, "")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()

	if _, err := NewAuditTrail(Config{DB: db, TableName: "bad name"}); err == nil {
		t.Fatal("expected error for invalid table name")
	}
}
