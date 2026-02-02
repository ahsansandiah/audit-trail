package audittrail

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// UserLoginActivity represents a user login session
type UserLoginActivity struct {
	ID           string     `json:"user_login_activity_id"`
	UserID       string     `json:"user_id"`
	LoginTime    time.Time  `json:"login_time"`
	LogoutTime   *time.Time `json:"logout_time,omitempty"`
	IPAddress    string     `json:"ip_address,omitempty"`
	UserAgent    string     `json:"user_agent,omitempty"`
	DeviceInfo   string     `json:"device_info,omitempty"`
	Location     string     `json:"location,omitempty"`
	SessionToken string     `json:"session_token,omitempty"`
}

// UserLoginActivityConfig holds configuration for UserLoginActivityRecorder
type UserLoginActivityConfig struct {
	DB          *sql.DB
	TableName   string
	Placeholder PlaceholderStyle
	Now         func() time.Time
}

// UserLoginActivityRecorder handles user login activity recording
type UserLoginActivityRecorder struct {
	db          *sql.DB
	table       string
	placeholder PlaceholderStyle
	now         func() time.Time
}

// NewUserLoginActivityRecorder creates a new UserLoginActivityRecorder
func NewUserLoginActivityRecorder(cfg UserLoginActivityConfig) (*UserLoginActivityRecorder, error) {
	if cfg.DB == nil {
		return nil, errors.New("audittrail: DB must not be nil")
	}

	table := cfg.TableName
	if table == "" {
		table = "log_user_login_activity"
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

	return &UserLoginActivityRecorder{
		db:          cfg.DB,
		table:       table,
		placeholder: placeholder,
		now:         nowFn,
	}, nil
}

// RecordLogin records a new user login activity and returns the generated ID
func (r *UserLoginActivityRecorder) RecordLogin(ctx context.Context, activity UserLoginActivity) (string, error) {
	if r == nil || r.db == nil {
		return "", errors.New("audittrail: instance is not initialized")
	}

	normalized, err := r.normalizeActivity(activity)
	if err != nil {
		return "", err
	}

	placeholders := r.buildPlaceholders(8)
	query := fmt.Sprintf(
		"INSERT INTO %s (user_login_activity_id, user_id, login_time, ip_address, user_agent, device_info, location, session_token) VALUES (%s)",
		r.table,
		placeholders,
	)

	_, err = r.db.ExecContext(
		ctx,
		query,
		normalized.ID,
		normalized.UserID,
		normalized.LoginTime,
		nullString(normalized.IPAddress),
		nullString(normalized.UserAgent),
		nullString(normalized.DeviceInfo),
		nullString(normalized.Location),
		nullString(normalized.SessionToken),
	)
	if err != nil {
		return "", err
	}

	return normalized.ID, nil
}

// RecordLogout updates the logout time for a login activity
func (r *UserLoginActivityRecorder) RecordLogout(ctx context.Context, activityID string) error {
	if r == nil || r.db == nil {
		return errors.New("audittrail: instance is not initialized")
	}

	if strings.TrimSpace(activityID) == "" {
		return errors.New("audittrail: activityID is required")
	}

	placeholder := "?"
	if r.placeholder == PlaceholderDollar {
		placeholder = "$1"
	}

	query := fmt.Sprintf(
		"UPDATE %s SET logout_time = %s WHERE user_login_activity_id = %s",
		r.table,
		placeholder,
		r.nextPlaceholder(placeholder),
	)

	logoutTime := r.now().UTC()
	_, err := r.db.ExecContext(ctx, query, logoutTime, activityID)
	return err
}

// GetByID retrieves a user login activity by ID
func (r *UserLoginActivityRecorder) GetByID(ctx context.Context, activityID string) (*UserLoginActivity, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("audittrail: instance is not initialized")
	}

	placeholder := "?"
	if r.placeholder == PlaceholderDollar {
		placeholder = "$1"
	}

	query := fmt.Sprintf(
		"SELECT user_login_activity_id, user_id, login_time, logout_time, ip_address, user_agent, device_info, location, session_token FROM %s WHERE user_login_activity_id = %s",
		r.table,
		placeholder,
	)

	var activity UserLoginActivity
	var logoutTime sql.NullTime
	var ipAddress, userAgent, deviceInfo, location, sessionToken sql.NullString

	err := r.db.QueryRowContext(ctx, query, activityID).Scan(
		&activity.ID,
		&activity.UserID,
		&activity.LoginTime,
		&logoutTime,
		&ipAddress,
		&userAgent,
		&deviceInfo,
		&location,
		&sessionToken,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	if logoutTime.Valid {
		activity.LogoutTime = &logoutTime.Time
	}
	activity.IPAddress = ipAddress.String
	activity.UserAgent = userAgent.String
	activity.DeviceInfo = deviceInfo.String
	activity.Location = location.String
	activity.SessionToken = sessionToken.String

	return &activity, nil
}

// GetBySessionToken retrieves a user login activity by session token
func (r *UserLoginActivityRecorder) GetBySessionToken(ctx context.Context, sessionToken string) (*UserLoginActivity, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("audittrail: instance is not initialized")
	}

	placeholder := "?"
	if r.placeholder == PlaceholderDollar {
		placeholder = "$1"
	}

	query := fmt.Sprintf(
		"SELECT user_login_activity_id, user_id, login_time, logout_time, ip_address, user_agent, device_info, location, session_token FROM %s WHERE session_token = %s AND logout_time IS NULL ORDER BY login_time DESC LIMIT 1",
		r.table,
		placeholder,
	)

	var activity UserLoginActivity
	var logoutTime sql.NullTime
	var ipAddress, userAgent, deviceInfo, location, sessToken sql.NullString

	err := r.db.QueryRowContext(ctx, query, sessionToken).Scan(
		&activity.ID,
		&activity.UserID,
		&activity.LoginTime,
		&logoutTime,
		&ipAddress,
		&userAgent,
		&deviceInfo,
		&location,
		&sessToken,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	if logoutTime.Valid {
		activity.LogoutTime = &logoutTime.Time
	}
	activity.IPAddress = ipAddress.String
	activity.UserAgent = userAgent.String
	activity.DeviceInfo = deviceInfo.String
	activity.Location = location.String
	activity.SessionToken = sessToken.String

	return &activity, nil
}

// EnsureTable creates the user_login_activity table if it doesn't exist
func (r *UserLoginActivityRecorder) EnsureTable(ctx context.Context) error {
	if r == nil || r.db == nil {
		return errors.New("audittrail: instance is not initialized")
	}

	query := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			user_login_activity_id VARCHAR(64) PRIMARY KEY,
			user_id VARCHAR(255) NOT NULL,
			login_time TIMESTAMP NOT NULL,
			logout_time TIMESTAMP NULL,
			ip_address VARCHAR(45) NULL,
			user_agent TEXT NULL,
			device_info TEXT NULL,
			location VARCHAR(255) NULL,
			session_token VARCHAR(255) NULL
		);`, r.table)

	_, err := r.db.ExecContext(ctx, query)
	if err != nil {
		return err
	}

	// Create index on session_token for faster lookups
	indexQuery := fmt.Sprintf(
		"CREATE INDEX IF NOT EXISTS idx_%s_session_token ON %s (session_token);",
		r.table, r.table,
	)
	_, _ = r.db.ExecContext(ctx, indexQuery) // Ignore error if index already exists

	// Create index on user_id for faster lookups
	userIndexQuery := fmt.Sprintf(
		"CREATE INDEX IF NOT EXISTS idx_%s_user_id ON %s (user_id);",
		r.table, r.table,
	)
	_, _ = r.db.ExecContext(ctx, userIndexQuery)

	return nil
}

func (r *UserLoginActivityRecorder) normalizeActivity(activity UserLoginActivity) (UserLoginActivity, error) {
	if strings.TrimSpace(activity.UserID) == "" {
		return UserLoginActivity{}, errors.New("audittrail: field UserID is required")
	}
	if activity.ID == "" {
		activity.ID = newID()
	}
	if activity.LoginTime.IsZero() {
		activity.LoginTime = r.now().UTC()
	}
	return activity, nil
}

func (r *UserLoginActivityRecorder) buildPlaceholders(n int) string {
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

func (r *UserLoginActivityRecorder) nextPlaceholder(current string) string {
	if r.placeholder == PlaceholderDollar {
		if current == "$1" {
			return "$2"
		}
		return "$1"
	}
	return "?"
}
