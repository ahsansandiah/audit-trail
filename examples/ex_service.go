package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	audittrail "github.com/ahsansandiah/audit-trail"

	// Database drivers - uncomment based on your database:
	// _ "github.com/jackc/pgx/v5/stdlib"  // PostgreSQL (pgx driver)
	// _ "github.com/lib/pq"               // PostgreSQL (pq driver)
	// _ "github.com/go-sql-driver/mysql"  // MySQL
	// _ "github.com/mattn/go-sqlite3"     // SQLite
)

func main() {
	// 1. Initialize audit trail with custom error handlers
	// Make sure environment variables are set (see .env.example)
	ctx := context.Background()

	err := audittrail.InitWithOptions(ctx, &audittrail.InitOptions{
		// Custom handler for consumer errors (e.g., DB insert error)
		OnConsumerError: func(err error) {
			log.Printf("[AUDIT-CONSUMER-ERROR] %v", err)
			// Add monitoring integration here:
			// sentry.CaptureException(err)
			// metrics.AuditConsumerErrors.Inc()
		},
		// Custom handler for Pub/Sub publish errors
		OnPublishError: func(err error) {
			log.Printf("[AUDIT-PUBLISH-ERROR] %v", err)
			// Add monitoring integration here:
			// sentry.CaptureException(err)
			// metrics.AuditPublishErrors.Inc()
		},
		// Optional: Secret Manager provider (uncomment if needed)
		// SecretProvider: provider,
	})
	if err != nil {
		log.Fatalf("Failed to initialize audit trail: %v", err)
	}

	// Alternative: Simple initialization without custom handlers (default: log.Printf)
	// if err := audittrail.InitFromEnv(ctx); err != nil {
	//     log.Fatalf("Failed to initialize audit trail: %v", err)
	// }
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := audittrail.Shutdown(shutdownCtx); err != nil {
			log.Printf("Audit trail shutdown error: %v", err)
		}
	}()

	// 2. Setup Gin router
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	// 3. Setup audit middleware (BEFORE routes)
	// This middleware will capture all request/response except skipped paths
	r.Use(audittrail.GinMiddleware(
		audittrail.WithServiceName("product-service"),                    // Your service name
		audittrail.WithSkipPaths("/health", "/metrics", "/api/v1/login"), // Paths to skip from audit
		audittrail.WithCaptureRequestBody(true),                          // Capture request body for POST/PUT/PATCH
		audittrail.WithCaptureResponseBody(true),                         // Capture response body for audit
		audittrail.WithMaxBodySize(2*1024*1024),                          // Max 2MB body size (for request & response)
		audittrail.WithGinErrorHandler(func(err error) {
			// Custom error handler if audit trail fails
			log.Printf("[AUDIT-ERROR] %v", err)
		}),
	))

	// 4. Public routes (no auth required)
	r.GET("/health", handleHealth)
	r.POST("/api/v1/login", handleLogin)
	r.POST("/api/v1/logout", handleLogout)

	// 5. Protected routes (auth required)
	authorized := r.Group("/api/v1")
	authorized.Use(authMiddleware()) // Sets user_id and user_login_activity_id to context
	{
		// Product endpoints
		authorized.GET("/products", handleListProducts)
		authorized.GET("/products/:id", handleGetProduct)
		authorized.POST("/products", handleCreateProduct)
		authorized.PUT("/products/:id", handleUpdateProduct)
		authorized.DELETE("/products/:id", handleDeleteProduct)

		// Order endpoints
		authorized.POST("/orders", handleCreateOrder)
		authorized.PUT("/orders/:id/status", handleUpdateOrderStatus)
		authorized.POST("/orders/:id/cancel", handleCancelOrder)
	}

	// 6. Start server with graceful shutdown
	srv := &http.Server{
		Addr:    ":8080",
		Handler: r,
	}

	// Start server in goroutine
	go func() {
		log.Println("🚀 Server starting on :8080")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("🛑 Shutting down server...")

	// Graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("✅ Server exited")
}

// ==================== Middleware ====================

// authMiddleware validates token and sets user_id and user_login_activity_id to context
// Audit middleware will automatically capture user_id and user_login_activity_id
func authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader("Authorization")

		if token == "" {
			c.AbortWithStatusJSON(401, gin.H{
				"error": "unauthorized",
				"code":  "MISSING_TOKEN",
			})
			return
		}

		// Simulate token validation (replace with JWT decode or actual validation)
		// Example: claims, err := jwt.Parse(token)
		if token != "Bearer valid-token-123" {
			c.AbortWithStatusJSON(401, gin.H{
				"error": "unauthorized",
				"code":  "INVALID_TOKEN",
			})
			return
		}

		// Extract user ID from token (simplified - use JWT claims in production)
		userID := "user-12345"

		// Set user_id to context - THIS IS IMPORTANT!
		// Audit middleware will get user_id from here for log_created_by field
		c.Set("user_id", userID)

		// Extract user_login_activity_id from token or session
		// In production, can be obtained from JWT claims or session store
		// Example: claims.LoginActivityID or redis.Get("session:"+token)
		loginActivityID := c.GetHeader("X-User-Login-Activity-Id")
		if loginActivityID != "" {
			// Set user_login_activity_id to context
			// Audit middleware will get this for user_login_activity_id field
			c.Set("user_login_activity_id", loginActivityID)
		}

		// Optional: set request_id if not present
		if c.GetHeader("X-Request-Id") == "" {
			c.Set("request_id", fmt.Sprintf("req-%d", time.Now().UnixNano()))
		}

		c.Next()
	}
}

// ==================== Handlers ====================

func handleHealth(c *gin.Context) {
	// Health check is not audited (already skipped)
	c.JSON(200, gin.H{
		"status": "ok",
		"time":   time.Now().Format(time.RFC3339),
	})
}

func handleLogin(c *gin.Context) {
	// Login is not audited (already skipped) for security reasons
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// Simulate login validation (replace with actual validation in production)
	userID := "user-12345"

	// Record user login activity
	// This will store login information and return the activity ID
	loginActivityID, err := audittrail.RecordLogin(c.Request.Context(), audittrail.UserLoginActivity{
		UserID:       userID,
		IPAddress:    c.ClientIP(),
		UserAgent:    c.GetHeader("User-Agent"),
		DeviceInfo:   c.GetHeader("X-Device-Info"), // Optional: sent from client
		Location:     "",                            // Optional: can be resolved from IP
		SessionToken: "valid-token-123",             // Store token for later lookup
	})
	if err != nil {
		log.Printf("[LOGIN-ACTIVITY-ERROR] %v", err)
		// Continue login even if recording activity fails
	}

	c.JSON(200, gin.H{
		"token": "Bearer valid-token-123",
		"user": gin.H{
			"id":       userID,
			"username": req.Username,
		},
		// Return login_activity_id so client can send it in subsequent request headers
		// Client should send "X-User-Login-Activity-Id" header in every request
		"login_activity_id": loginActivityID,
	})
}

func handleLogout(c *gin.Context) {
	// Get login activity ID from header or request body
	loginActivityID := c.GetHeader("X-User-Login-Activity-Id")
	if loginActivityID == "" {
		var req struct {
			LoginActivityID string `json:"login_activity_id"`
		}
		if err := c.ShouldBindJSON(&req); err == nil {
			loginActivityID = req.LoginActivityID
		}
	}

	if loginActivityID != "" {
		// Record logout time
		if err := audittrail.RecordLogout(c.Request.Context(), loginActivityID); err != nil {
			log.Printf("[LOGOUT-ACTIVITY-ERROR] %v", err)
		}
	}

	c.JSON(200, gin.H{
		"message": "logged out successfully",
	})
}

func handleListProducts(c *gin.Context) {
	// Query params are automatically captured via endpoint
	// Audit record: log_action = "GET /api/v1/products"

	products := []map[string]any{
		{"id": "prod-1", "name": "Product A", "price": 100},
		{"id": "prod-2", "name": "Product B", "price": 200},
	}

	c.JSON(200, gin.H{
		"data":  products,
		"total": len(products),
	})
}

func handleGetProduct(c *gin.Context) {
	productID := c.Param("id")

	// Audit record: log_action = "GET /api/v1/products/:id"
	// log_endpoint = "/api/v1/products/prod-1"

	product := map[string]any{
		"id":    productID,
		"name":  "Product A",
		"price": 100,
		"stock": 50,
	}

	c.JSON(200, product)
}

func handleCreateProduct(c *gin.Context) {
	var req struct {
		Name  string  `json:"name" binding:"required"`
		Price float64 `json:"price" binding:"required"`
		Stock int     `json:"stock" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// Optional: Set custom action name (more descriptive)
	c.Set("audit_action", "CREATE_PRODUCT")

	// Business logic - create product
	newProduct := map[string]any{
		"id":    fmt.Sprintf("prod-%d", time.Now().Unix()),
		"name":  req.Name,
		"price": req.Price,
		"stock": req.Stock,
	}

	c.JSON(201, newProduct)

	// Stored audit record:
	// - log_created_by: "user-12345" (from context)
	// - log_action: "CREATE_PRODUCT" (custom)
	// - log_endpoint: "/api/v1/products"
	// - log_request: {"name":"Product A","price":100,"stock":50}
	// - log_response: {"id":"prod-123","name":"Product A","price":100,"stock":50} (auto-captured)
}

func handleUpdateProduct(c *gin.Context) {
	productID := c.Param("id")

	var req struct {
		Name  string  `json:"name"`
		Price float64 `json:"price"`
		Stock int     `json:"stock"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// Set custom action
	c.Set("audit_action", "UPDATE_PRODUCT")

	updatedProduct := map[string]any{
		"id":    productID,
		"name":  req.Name,
		"price": req.Price,
		"stock": req.Stock,
	}

	c.JSON(200, updatedProduct)
}

func handleDeleteProduct(c *gin.Context) {
	_ = c.Param("id") // product ID for delete operation

	// Set custom action
	c.Set("audit_action", "DELETE_PRODUCT")

	// Business logic - delete product
	// ...

	c.JSON(204, nil)

	// Audit record:
	// - log_action: "DELETE_PRODUCT"
	// - log_endpoint: "/api/v1/products/prod-1"
	// - log_request: null (DELETE usually has no body)
}

func handleCreateOrder(c *gin.Context) {
	var req struct {
		ProductID string `json:"product_id" binding:"required"`
		Quantity  int    `json:"quantity" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	c.Set("audit_action", "CREATE_ORDER")

	order := map[string]any{
		"id":         fmt.Sprintf("order-%d", time.Now().Unix()),
		"product_id": req.ProductID,
		"quantity":   req.Quantity,
		"status":     "pending",
		"created_at": time.Now().Format(time.RFC3339),
	}

	c.JSON(201, order)
}

func handleUpdateOrderStatus(c *gin.Context) {
	orderID := c.Param("id")

	var req struct {
		Status string `json:"status" binding:"required,oneof=pending processing shipped delivered cancelled"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// More specific custom action
	c.Set("audit_action", fmt.Sprintf("UPDATE_ORDER_STATUS_%s", req.Status))

	c.JSON(200, gin.H{
		"id":     orderID,
		"status": req.Status,
	})
}

func handleCancelOrder(c *gin.Context) {
	orderID := c.Param("id")

	var req struct {
		Reason string `json:"reason" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	c.Set("audit_action", "CANCEL_ORDER")

	c.JSON(200, gin.H{
		"id":     orderID,
		"status": "cancelled",
		"reason": req.Reason,
	})

	// Audit record will capture reason in log_request
}
