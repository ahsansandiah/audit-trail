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
	ID        string    `json:"id"`
	RequestID string    `json:"request_id,omitempty"`
	Actor     string    `json:"actor,omitempty"`
	Action    string    `json:"action"`
	Endpoint  string    `json:"endpoint,omitempty"`
	Request   any       `json:"request,omitempty"`
	Response  any       `json:"response,omitempty"`
	IPAddress string    `json:"ip_address,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by,omitempty"`
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

	placeholders := r.buildPlaceholders(10)
	query := fmt.Sprintf(
		"INSERT INTO %s (id, request_id, actor, action, endpoint, request, response, ip_address, created_at, created_by) VALUES (%s)",
		r.table,
		placeholders,
	)

	_, err = r.db.ExecContext(
		ctx,
		query,
		normalized.ID,
		nullString(normalized.RequestID),
		nullString(normalized.Actor),
		normalized.Action,
		nullString(normalized.Endpoint),
		requestValue,
		responseValue,
		nullString(normalized.IPAddress),
		normalized.CreatedAt,
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
				id VARCHAR(64) PRIMARY KEY,
				request_id VARCHAR(128) NULL,
				actor VARCHAR(255) NULL,
				action VARCHAR(255) NOT NULL,
				endpoint TEXT NULL,
				request TEXT NULL,
				response TEXT NULL,
				ip_address VARCHAR(64) NULL,
				created_at TIMESTAMP NOT NULL,
				created_by VARCHAR(255) NULL
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
	if entry.CreatedAt.IsZero() {
		if now == nil {
			now = time.Now
		}
		entry.CreatedAt = now().UTC()
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
	case strings.Contains(driverName, "pq"), strings.Contains(driverName, "pgx"):
		return PlaceholderDollar
	default:
		return PlaceholderQuestion
	}
}
