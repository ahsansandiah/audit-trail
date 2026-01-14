package audittrail

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

type PlaceholderStyle int

const (
	PlaceholderUnknown  PlaceholderStyle = iota
	PlaceholderQuestion                  // MySQL, SQLite, etc -> "?"
	PlaceholderDollar                    // Postgres -> "$1"
)

type Config struct {
	DB          *sql.DB
	TableName   string
	Placeholder PlaceholderStyle
	Now         func() time.Time
}

type Recorder interface {
	Record(ctx context.Context, entry Entry) error
}

type RecorderFunc func(context.Context, Entry) error

func (f RecorderFunc) Record(ctx context.Context, entry Entry) error { return f(ctx, entry) }

type Entry struct {
	ID          string    `json:"log_audit_trail_id"`
	RequestID   string    `json:"log_req_id,omitempty"`
	Action      string    `json:"log_action"`
	Endpoint    string    `json:"log_endpoint,omitempty"`
	Request     any       `json:"log_request,omitempty"`
	Response    any       `json:"log_response,omitempty"`
	CreatedDate time.Time `json:"log_created_date"`
	CreatedBy   string    `json:"log_created_by,omitempty"`
}

type AuditTrail struct {
	db          *sql.DB
	table       string
	placeholder PlaceholderStyle
	now         func() time.Time
}

func NewAuditTrail(cfg Config) (*AuditTrail, error) {
	if cfg.DB == nil {
		return nil, errors.New("audittrail: DB must not be nil")
	}

	table := cfg.TableName
	if table == "" {
		table = "audit_trail"
	}
	if !isSafeIdentifier(table) {
		return nil, fmt.Errorf("audittrail: invalid table name: %s", table)
	}

	placeholder := cfg.Placeholder
	if placeholder == PlaceholderUnknown {
		placeholder = detectPlaceholder(cfg.DB)
	}
	if placeholder == PlaceholderUnknown {
		placeholder = PlaceholderQuestion
	}

	nowFn := cfg.Now
	if nowFn == nil {
		nowFn = time.Now
	}

	return &AuditTrail{
		db:          cfg.DB,
		table:       table,
		placeholder: placeholder,
		now:         nowFn,
	}, nil
}

func (r *AuditTrail) Record(ctx context.Context, entry Entry) error {
	if r == nil || r.db == nil {
		return errors.New("audittrail: instance is not initialized")
	}
	normalized, err := normalizeEntry(entry, r.now)
	if err != nil {
		return err
	}

	requestValue, err := marshalJSONValue(normalized.Request)
	if err != nil {
		return fmt.Errorf("audittrail: marshal request failed: %w", err)
	}
	responseValue, err := marshalJSONValue(normalized.Response)
	if err != nil {
		return fmt.Errorf("audittrail: marshal response failed: %w", err)
	}

	placeholders := r.buildPlaceholders(8)
	query := fmt.Sprintf(
		"INSERT INTO %s (log_audit_trail_id, log_req_id, log_action, log_endpoint, log_request, log_response, log_created_date, log_created_by) VALUES (%s)",
		r.table,
		placeholders,
	)

	_, err = r.db.ExecContext(
		ctx,
		query,
		normalized.ID,
		nullString(normalized.RequestID),
		normalized.Action,
		nullString(normalized.Endpoint),
		requestValue,
		responseValue,
		normalized.CreatedDate,
		nullString(normalized.CreatedBy),
	)
	return err
}

func (r *AuditTrail) EnsureTable(ctx context.Context) error {
	if r == nil || r.db == nil {
		return errors.New("audittrail: instance is not initialized")
	}

	query := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			log_audit_trail_id VARCHAR(64) PRIMARY KEY,
			log_req_id VARCHAR(128) NULL,
			log_action VARCHAR(255) NOT NULL,
			log_endpoint TEXT NULL,
			log_request JSON NULL,
			log_response JSON NULL,
			log_created_date TIMESTAMP NOT NULL,
			log_created_by VARCHAR(255) NULL
		);`, r.table)

	_, err := r.db.ExecContext(ctx, query)
	return err
}

func (r *AuditTrail) buildPlaceholders(n int) string {
	switch r.placeholder {
	case PlaceholderDollar:
		parts := make([]string, n)
		for i := 0; i < n; i++ {
			parts[i] = fmt.Sprintf("$%d", i+1)
		}
		return strings.Join(parts, ", ")
	default:
		parts := make([]string, n)
		for i := range parts {
			parts[i] = "?"
		}
		return strings.Join(parts, ", ")
	}
}

func marshalJSONValue(v any) (sql.NullString, error) {
	if v == nil {
		return sql.NullString{}, nil
	}

	switch val := v.(type) {
	case json.RawMessage:
		return sql.NullString{String: string(val), Valid: true}, nil
	case []byte:
		if len(val) == 0 {
			return sql.NullString{}, nil
		}
		return sql.NullString{String: string(val), Valid: true}, nil
	case string:
		if strings.TrimSpace(val) == "" {
			return sql.NullString{}, nil
		}
		return sql.NullString{String: val, Valid: true}, nil
	default:
		buf, err := json.Marshal(v)
		if err != nil {
			return sql.NullString{}, fmt.Errorf("audittrail: marshal JSON failed: %w", err)
		}
		return sql.NullString{String: string(buf), Valid: true}, nil
	}
}

func normalizeEntry(entry Entry, now func() time.Time) (Entry, error) {
	if strings.TrimSpace(entry.Action) == "" {
		return Entry{}, errors.New("audittrail: field Action is required")
	}
	if entry.ID == "" {
		entry.ID = newID()
	}
	if entry.CreatedDate.IsZero() {
		if now == nil {
			now = time.Now
		}
		entry.CreatedDate = now().UTC()
	}
	return entry, nil
}

func nullString(s string) sql.NullString {
	if strings.TrimSpace(s) == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err == nil {
		return hex.EncodeToString(b[:])
	}
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func isSafeIdentifier(name string) bool {
	return regexp.MustCompile(`^[a-zA-Z0-9_]+$`).MatchString(name)
}

func detectPlaceholder(db *sql.DB) PlaceholderStyle {
	if db == nil {
		return PlaceholderUnknown
	}

	driverName := strings.ToLower(fmt.Sprintf("%T", db.Driver()))
	switch {
	case strings.Contains(driverName, "pq"),
		strings.Contains(driverName, "pgx"),
		strings.Contains(driverName, "stdlib.driver"), // pgx/v5/stdlib
		strings.Contains(driverName, "postgres"):
		return PlaceholderDollar
	default:
		return PlaceholderQuestion
	}
}

// detectPlaceholderFromDriver detects placeholder style from driver name string
func detectPlaceholderFromDriver(driver string) PlaceholderStyle {
	driver = strings.ToLower(driver)
	switch {
	case strings.Contains(driver, "pgx"),
		strings.Contains(driver, "pq"),
		strings.Contains(driver, "postgres"):
		return PlaceholderDollar
	case strings.Contains(driver, "mysql"),
		strings.Contains(driver, "sqlite"):
		return PlaceholderQuestion
	default:
		return PlaceholderUnknown
	}
}
