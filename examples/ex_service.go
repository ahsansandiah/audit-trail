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

	// Database drivers - uncomment sesuai database yang Anda pakai:
	// _ "github.com/jackc/pgx/v5/stdlib"  // PostgreSQL (pgx driver)
	// _ "github.com/lib/pq"               // PostgreSQL (pq driver)
	// _ "github.com/go-sql-driver/mysql"  // MySQL
	// _ "github.com/mattn/go-sqlite3"     // SQLite
)

func main() {
	// 1. Initialize audit trail from environment variables
	// Pastikan environment variables sudah di-set (lihat .env.example)
	ctx := context.Background()
	if err := audittrail.InitFromEnv(ctx); err != nil {
		log.Fatalf("Failed to initialize audit trail: %v", err)
	}
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
	// Middleware ini akan capture semua request/response kecuali yang di-skip
	r.Use(audittrail.GinMiddleware(
		audittrail.WithServiceName("product-service"), // Nama service Anda
		audittrail.WithSkipPaths("/health", "/metrics", "/api/v1/login"), // Skip paths yang tidak perlu di-audit
		audittrail.WithCaptureRequestBody(true),       // Capture request body untuk POST/PUT/PATCH
		audittrail.WithMaxBodySize(2*1024*1024),       // Max 2MB body size
		audittrail.WithGinErrorHandler(func(err error) {
			// Custom error handler jika audit trail gagal
			log.Printf("[AUDIT-ERROR] %v", err)
		}),
	))

	// 4. Public routes (tidak perlu auth)
	r.GET("/health", handleHealth)
	r.POST("/api/v1/login", handleLogin)

	// 5. Protected routes (perlu auth)
	authorized := r.Group("/api/v1")
	authorized.Use(authMiddleware()) // Set user_id ke context
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

// authMiddleware validates token and sets user_id to context
// Audit middleware akan otomatis capture user_id ini
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

		// Simulate token validation (ganti dengan JWT decode atau validasi sebenarnya)
		// Example: claims, err := jwt.Parse(token)
		if token != "Bearer valid-token-123" {
			c.AbortWithStatusJSON(401, gin.H{
				"error": "unauthorized",
				"code":  "INVALID_TOKEN",
			})
			return
		}

		// Extract user ID dari token (simplified - gunakan JWT claims di production)
		userID := "user-12345"

		// Set user_id ke context - INI PENTING!
		// Audit middleware akan ambil user_id dari sini untuk field log_created_by
		c.Set("user_id", userID)

		// Optional: set request_id jika belum ada
		if c.GetHeader("X-Request-Id") == "" {
			c.Set("request_id", fmt.Sprintf("req-%d", time.Now().UnixNano()))
		}

		c.Next()
	}
}

// ==================== Handlers ====================

func handleHealth(c *gin.Context) {
	// Health check tidak di-audit (sudah di-skip)
	c.JSON(200, gin.H{
		"status": "ok",
		"time":   time.Now().Format(time.RFC3339),
	})
}

func handleLogin(c *gin.Context) {
	// Login tidak di-audit (sudah di-skip) karena security concern
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// Simulate login logic
	c.JSON(200, gin.H{
		"token": "Bearer valid-token-123",
		"user": gin.H{
			"id":       "user-12345",
			"username": req.Username,
		},
	})
}

func handleListProducts(c *gin.Context) {
	// Query params otomatis ter-capture via endpoint
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

	// Optional: Set custom action name (lebih descriptive)
	c.Set("audit_action", "CREATE_PRODUCT")

	// Business logic - create product
	newProduct := map[string]any{
		"id":    fmt.Sprintf("prod-%d", time.Now().Unix()),
		"name":  req.Name,
		"price": req.Price,
		"stock": req.Stock,
	}

	c.JSON(201, newProduct)

	// Audit record yang tersimpan:
	// - log_created_by: "user-12345" (dari context)
	// - log_action: "CREATE_PRODUCT" (custom)
	// - log_endpoint: "/api/v1/products"
	// - log_request: {"name":"Product A","price":100,"stock":50}
	// - log_response: null (default, bisa di-custom)
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
	_ = c.Param("id") // product ID untuk delete operation

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

	// Custom action yang lebih specific
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

	// Audit record akan capture reason di log_request
}
