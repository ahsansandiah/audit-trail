package audittrail

import (
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

// HTTPMiddlewareOption configures HTTPMiddleware behavior.
type HTTPMiddlewareOption func(*httpMiddlewareConfig)

type httpMiddlewareConfig struct {
	requestIDHeader string
	actorHeader     string
	ipHeader        string
	action          func(*http.Request) string
	requestPayload  func(*http.Request) any
	responsePayload func(int) any
	onError         func(error)
	now             func() time.Time
}

func defaultHTTPConfig() httpMiddlewareConfig {
	return httpMiddlewareConfig{
		requestIDHeader: "X-Request-Id",
		actorHeader:     "X-User-Id",
		ipHeader:        "X-Forwarded-For",
		action: func(r *http.Request) string {
			return strings.TrimSpace(r.Method + " " + r.URL.Path)
		},
		requestPayload: func(_ *http.Request) any {
			return nil
		},
		responsePayload: nil,
		onError: func(err error) {
			log.Printf("audittrail: middleware record failed: %v", err)
		},
		now: time.Now,
	}
}

// HTTPMiddleware returns a standard net/http middleware that records audit trail entries
// after the wrapped handler finishes. It captures request metadata via options.
func HTTPMiddleware(recorder Recorder, opts ...HTTPMiddlewareOption) func(http.Handler) http.Handler {
	if recorder == nil {
		panic("audittrail: HTTPMiddleware requires a non-nil Recorder")
	}

	cfg := defaultHTTPConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			start := cfg.now().UTC()

			next.ServeHTTP(rec, r)

			entry := Entry{
				RequestID:   headerValue(r, cfg.requestIDHeader),
				Action:      cfg.action(r),
				Endpoint:    r.URL.Path,
				Request:     cfg.requestPayload(r),
				Response:    nil,
				CreatedDate: start,
				CreatedBy:   headerValue(r, cfg.actorHeader),
			}
			if cfg.responsePayload != nil {
				entry.Response = cfg.responsePayload(rec.status)
			}

			if err := recorder.Record(r.Context(), entry); err != nil && cfg.onError != nil {
				cfg.onError(err)
			}
		})
	}
}

// WithRequestIDHeader overrides which header is used as the request ID. Default: X-Request-Id.
func WithRequestIDHeader(name string) HTTPMiddlewareOption {
	return func(c *httpMiddlewareConfig) {
		c.requestIDHeader = name
	}
}

// WithActorHeader sets which header contains the actor/user ID. Default: X-User-Id.
func WithActorHeader(name string) HTTPMiddlewareOption {
	return func(c *httpMiddlewareConfig) {
		c.actorHeader = name
	}
}

// WithIPHeader sets which header contains the client IP. Default: X-Forwarded-For.
func WithIPHeader(name string) HTTPMiddlewareOption {
	return func(c *httpMiddlewareConfig) {
		c.ipHeader = name
	}
}

// WithAction customizes how the Action field is generated.
func WithAction(fn func(*http.Request) string) HTTPMiddlewareOption {
	return func(c *httpMiddlewareConfig) {
		if fn != nil {
			c.action = fn
		}
	}
}

// WithRequestPayload sets how the request payload is extracted for storage (e.g., headers/body).
func WithRequestPayload(fn func(*http.Request) any) HTTPMiddlewareOption {
	return func(c *httpMiddlewareConfig) {
		if fn != nil {
			c.requestPayload = fn
		}
	}
}

// WithResponsePayload sets how the response payload is derived (default captures status code).
func WithResponsePayload(fn func(status int) any) HTTPMiddlewareOption {
	return func(c *httpMiddlewareConfig) {
		if fn != nil {
			c.responsePayload = fn
		}
	}
}

// WithNow overrides the clock used for timestamps (useful in tests).
func WithNow(fn func() time.Time) HTTPMiddlewareOption {
	return func(c *httpMiddlewareConfig) {
		if fn != nil {
			c.now = fn
		}
	}
}

// WithErrorHandler overrides how middleware errors are reported.
func WithErrorHandler(fn func(error)) HTTPMiddlewareOption {
	return func(c *httpMiddlewareConfig) {
		c.onError = fn
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func headerValue(r *http.Request, name string) string {
	if name == "" {
		return ""
	}
	return strings.TrimSpace(r.Header.Get(name))
}

func clientIP(r *http.Request, header string) string {
	if header != "" {
		raw := r.Header.Get(header)
		if raw != "" {
			parts := strings.Split(raw, ",")
			if len(parts) > 0 {
				return strings.TrimSpace(parts[0])
			}
		}
	}

	if host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr)); err == nil {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}
