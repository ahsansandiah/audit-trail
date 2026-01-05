package audittrail

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"os"
	"sync"

	"github.com/gin-gonic/gin"
)

var ginInitOnce sync.Once

// GinMiddleware returns Gin-compatible middleware for audit trail
// This is a thin adapter that uses the framework-agnostic BuildEntry function
func GinMiddleware(opts ...GinMiddlewareOption) gin.HandlerFunc {
	cfg := defaultGinConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	return func(c *gin.Context) {
		// Skip if needed
		if cfg.shouldSkip != nil && cfg.shouldSkip(c) {
			c.Next()
			return
		}

		// 1. Capture request body (for POST/PUT/PATCH)
		var requestBody any
		if shouldCaptureBody(c.Request.Method) && cfg.captureRequestBody {
			requestBody = captureRequestPayload(c, cfg.maxBodySize)
		}

		// 2. Extract user ID dari context (set oleh auth middleware)
		userID := cfg.extractUser(c)

		// 3. Extract request ID
		requestID := c.GetHeader("X-Request-Id")
		if requestID == "" {
			if rid, exists := c.Get("request_id"); exists {
				requestID = rid.(string)
			}
		}

		// 4. Process request
		c.Next()

		// 5. Get custom action name (optional)
		action := ""
		if a, exists := c.Get("audit_action"); exists {
			action = a.(string)
		}

		// 6. Build entry using framework-agnostic helper
		entry := BuildEntry(
			HTTPRequest{
				Method: c.Request.Method,
				Path:   c.Request.URL.Path,
				Body:   requestBody,
			},
			HTTPResponse{
				StatusCode: c.Writer.Status(),
			},
			RequestContext{
				UserID:      userID,
				RequestID:   requestID,
				Action:      action,
				ServiceName: cfg.serviceName,
			},
		)

		// 7. Record async (non-blocking)
		go func() {
			if err := Record(c.Request.Context(), entry); err != nil {
				if cfg.onError != nil {
					cfg.onError(err)
				}
			}
		}()
	}
}

// AutoGinMiddleware automatically initializes audit trail on first use
func AutoGinMiddleware(opts ...GinMiddlewareOption) gin.HandlerFunc {
	ginInitOnce.Do(func() {
		if os.Getenv("AUDIT_AUTO_INIT") != "true" {
			return
		}

		ctx := context.Background()
		if err := InitFromEnv(ctx); err != nil {
			log.Printf("audittrail: auto-init failed: %v", err)
			return
		}

		log.Println("audittrail: auto-initialized for Gin")
	})

	return GinMiddleware(opts...)
}

// GinMiddlewareOption configures Gin middleware
type GinMiddlewareOption func(*ginMiddlewareConfig)

type ginMiddlewareConfig struct {
	captureRequestBody bool
	maxBodySize        int64
	extractUser        func(*gin.Context) string
	serviceName        string
	shouldSkip         func(*gin.Context) bool
	onError            func(error)
}

func defaultGinConfig() ginMiddlewareConfig {
	return ginMiddlewareConfig{
		captureRequestBody: true,
		maxBodySize:        1024 * 1024, // 1MB
		extractUser: func(c *gin.Context) string {
			// Priority 1: dari context (set oleh auth middleware)
			if userID, exists := c.Get("user_id"); exists {
				if id, ok := userID.(string); ok {
					return id
				}
			}
			// Priority 2: dari header
			return c.GetHeader("X-User-Id")
		},
		serviceName: "unknown",
		shouldSkip: func(c *gin.Context) bool {
			// Default: skip health check
			return c.Request.URL.Path == "/health"
		},
		onError: func(err error) {
			log.Printf("audittrail: %v", err)
		},
	}
}

// WithCaptureRequestBody enables/disables request body capture
func WithCaptureRequestBody(capture bool) GinMiddlewareOption {
	return func(c *ginMiddlewareConfig) {
		c.captureRequestBody = capture
	}
}

// WithMaxBodySize sets max request body size to capture (in bytes)
func WithMaxBodySize(size int64) GinMiddlewareOption {
	return func(c *ginMiddlewareConfig) {
		c.maxBodySize = size
	}
}

// WithUserExtractor sets custom user extraction logic
func WithUserExtractor(fn func(*gin.Context) string) GinMiddlewareOption {
	return func(c *ginMiddlewareConfig) {
		if fn != nil {
			c.extractUser = fn
		}
	}
}

// WithServiceName sets service name for CreatedBy field
func WithServiceName(name string) GinMiddlewareOption {
	return func(c *ginMiddlewareConfig) {
		c.serviceName = name
	}
}

// WithSkipPaths sets paths to skip from audit
func WithSkipPaths(paths ...string) GinMiddlewareOption {
	pathMap := make(map[string]bool)
	for _, p := range paths {
		pathMap[p] = true
	}
	return func(c *ginMiddlewareConfig) {
		c.shouldSkip = func(ctx *gin.Context) bool {
			return pathMap[ctx.Request.URL.Path]
		}
	}
}

// WithSkipFunc sets custom skip logic
func WithSkipFunc(fn func(*gin.Context) bool) GinMiddlewareOption {
	return func(c *ginMiddlewareConfig) {
		if fn != nil {
			c.shouldSkip = fn
		}
	}
}

// WithErrorHandler sets custom error handler
func WithGinErrorHandler(fn func(error)) GinMiddlewareOption {
	return func(c *ginMiddlewareConfig) {
		if fn != nil {
			c.onError = fn
		}
	}
}

// Helper functions

func shouldCaptureBody(method string) bool {
	return method == "POST" || method == "PUT" || method == "PATCH"
}

func captureRequestPayload(c *gin.Context, maxSize int64) any {
	if c.Request.Body == nil {
		return nil
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(c.Request.Body, maxSize))
	if err != nil {
		return nil
	}

	// Restore body so handler can read it
	c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	// Try parse as JSON
	var payload any
	if err := json.Unmarshal(bodyBytes, &payload); err != nil {
		// If not JSON, return as string
		return string(bodyBytes)
	}

	return payload
}
