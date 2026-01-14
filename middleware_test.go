package audittrail

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPMiddlewareRecordsEntry(t *testing.T) {
	var calls []execCall

	driverName := fmt.Sprintf("audittrail_stub_mw_%d", time.Now().UnixNano())
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

	fixedTime := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	audit, err := NewAuditTrail(Config{DB: db, Placeholder: PlaceholderQuestion})
	if err != nil {
		t.Fatalf("NewAuditTrail: %v", err)
	}

	mw := HTTPMiddleware(
		audit,
		WithNow(func() time.Time { return fixedTime }),
		WithErrorHandler(func(err error) { t.Fatalf("unexpected error: %v", err) }),
	)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/orders", nil)
	req.Header.Set("X-Request-Id", "req-123")
	req.Header.Set("X-User-Id", "user-9")
	req.Header.Set("X-Forwarded-For", "10.0.0.8")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if len(calls) != 1 {
		t.Fatalf("expected 1 DB call, got %d", len(calls))
	}

	args := calls[0].args
	// New column order: log_audit_trail_id, log_req_id, log_action, log_endpoint, log_request, log_response, log_created_date, log_created_by
	if got := stringArg(args, 1); got != "req-123" {
		t.Fatalf("request_id mismatch: %q", got)
	}
	if got := stringArg(args, 2); got != "POST /api/orders" {
		t.Fatalf("action mismatch: %q", got)
	}
	if got := stringArg(args, 3); got != "/api/orders" {
		t.Fatalf("endpoint mismatch: %q", got)
	}
	if got := args[6].Value.(time.Time); !got.Equal(fixedTime) {
		t.Fatalf("created_date mismatch: %v", got)
	}
	if got := stringArg(args, 7); got != "user-9" {
		t.Fatalf("created_by mismatch: %q", got)
	}

	resp := stringArg(args, 5)
	if resp != "" {
		var decoded map[string]any
		if err := json.Unmarshal([]byte(resp), &decoded); err != nil {
			t.Fatalf("response JSON: %v", err)
		}
		t.Fatalf("response should be empty by default")
	}
}

func stringArg(args []driver.NamedValue, idx int) string {
	switch v := args[idx].Value.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	case time.Time:
		return v.String()
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", v)
	}
}
