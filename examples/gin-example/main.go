package main

import (
	"context"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	_ "github.com/jackc/pgx/v5/stdlib"

	audittrail "github.com/ahsansandiah/audit-trail"
)

func main() {
	// Initialize audit trail from environment variables
	ctx := context.Background()
	if err := audittrail.InitFromEnv(ctx); err != nil {
		log.Fatal(err)
	}
	defer audittrail.Shutdown(ctx)

	r := gin.Default()

	// Setup audit middleware SEKALI untuk semua routes
	r.Use(audittrail.GinMiddleware(
		audittrail.WithServiceName("order-service"),
		audittrail.WithSkipPaths("/health", "/login"),
		audittrail.WithCaptureRequestBody(true),
	))

	// Public routes (tidak perlu auth)
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	r.POST("/login", handleLogin)

	// Protected routes (perlu auth)
	authorized := r.Group("/")
	authorized.Use(authMiddleware()) // Set user_id ke context
	{
		authorized.POST("/orders", handleCreateOrder)
		authorized.PUT("/orders/:id", handleUpdateOrder)
		authorized.DELETE("/orders/:id", handleDeleteOrder)
	}

	log.Println("Server running on :8080")
	r.Run(":8080")
}

// authMiddleware validates token and sets user_id to context
func authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader("Authorization")

		// Simulate token validation
		if token == "" {
			c.AbortWithStatusJSON(401, gin.H{"error": "unauthorized"})
			return
		}

		// Extract user ID from token (simplified)
		userID := "user-123" // In real app: extract from JWT

		// Set user_id ke context - ini yang akan di-capture oleh audit middleware
		c.Set("user_id", userID)

		c.Next()
	}
}

func handleLogin(c *gin.Context) {
	// Login tidak di-audit (di-skip)
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{
		"user":  gin.H{"id": "user-123", "name": "John Doe"},
		"token": "fake-jwt-token",
	})
}

func handleCreateOrder(c *gin.Context) {
	// Request body otomatis ter-capture
	// User ID otomatis ter-capture dari context
	var req struct {
		ProductID string  `json:"product_id"`
		Quantity  int     `json:"quantity"`
		Price     float64 `json:"price"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// Optional: set custom action name
	c.Set("audit_action", "CREATE_ORDER")

	// Business logic
	order := map[string]any{
		"id":         "order-789",
		"product_id": req.ProductID,
		"quantity":   req.Quantity,
		"status":     "pending",
	}

	c.JSON(http.StatusCreated, order)

	// Audit otomatis ter-record dengan:
	// - log_created_by: "user-123" (dari context)
	// - log_action: "CREATE_ORDER"
	// - log_request: {"product_id":"...", "quantity":2, "price":100}
	// - log_endpoint: "/orders"
}

func handleUpdateOrder(c *gin.Context) {
	orderID := c.Param("id")

	var req struct {
		Status string `json:"status"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	c.Set("audit_action", "UPDATE_ORDER")

	c.JSON(200, gin.H{
		"id":     orderID,
		"status": req.Status,
	})
}

func handleDeleteOrder(c *gin.Context) {
	orderID := c.Param("id")

	c.Set("audit_action", "DELETE_ORDER")

	c.JSON(204, nil)

	// Audit record:
	// - log_created_by: "user-123"
	// - log_action: "DELETE_ORDER"
	// - log_endpoint: "/orders/order-789"
}
