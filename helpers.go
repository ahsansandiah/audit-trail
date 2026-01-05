package audittrail

import (
	"time"
)

// HTTPRequest represents a generic HTTP request (framework agnostic)
type HTTPRequest struct {
	Method   string
	Path     string
	Body     any
	Headers  map[string]string
	ClientIP string
}

// HTTPResponse represents a generic HTTP response
type HTTPResponse struct {
	StatusCode int
	Body       any
}

// RequestContext holds context data for audit entry
type RequestContext struct {
	UserID      string // User ID yang melakukan request (untuk CreatedBy)
	RequestID   string // Request ID
	Action      string // Custom action name (optional)
	ServiceName string // Service name
}

// BuildEntry creates audit entry from HTTP context (framework agnostic)
// This function can be used by any framework adapter
func BuildEntry(req HTTPRequest, resp HTTPResponse, ctx RequestContext) Entry {
	action := ctx.Action
	if action == "" {
		action = req.Method + " " + req.Path
	}

	return Entry{
		RequestID:   ctx.RequestID,
		Action:      action,
		Endpoint:    req.Path,
		Request:     req.Body,
		Response:    resp.Body,
		CreatedDate: time.Now().UTC(),
		CreatedBy:   ctx.UserID,
	}
}

// RecordAsync records audit entry asynchronously (non-blocking)
func RecordAsync(entry Entry) {
	go func() {
		if err := Record(nil, entry); err != nil {
			// Error already logged by Record()
		}
	}()
}
