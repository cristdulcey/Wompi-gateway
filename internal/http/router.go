package http

import (
	_ "embed"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/loducode/wompi-gateway/internal/dispatcher"
	"github.com/loducode/wompi-gateway/internal/domain"
)

//go:embed ui/index.html
var adminDashboardHTML []byte

// NewRouter builds and returns the configured Gin HTTP router.
func NewRouter(repo domain.Repository, disp *dispatcher.Dispatcher, adminAPIKey string, ginMode string) *gin.Engine {
	if ginMode != "" {
		gin.SetMode(ginMode)
	}

	r := gin.New()
	r.Use(gin.Recovery())

	h := NewHandler(repo, disp)

	// Health & Root redirect
	r.GET("/healthz", h.HealthCheck)
	r.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusTemporaryRedirect, "/admin")
	})

	// Embedded Admin Dashboard
	r.GET("/admin", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", adminDashboardHTML)
	})

	// Public Wompi Webhook Ingress
	r.POST("/webhooks/wompi/:account_slug", h.ReceiveWompiWebhook)

	// Admin Session Auth Endpoints (HttpOnly cookie login/logout)
	r.POST("/api/v1/auth/login", AdminLoginHandler(adminAPIKey))
	r.POST("/api/v1/auth/logout", AdminLogoutHandler())

	// Protected Admin API v1
	v1 := r.Group("/api/v1")
	v1.Use(AdminAuthMiddleware(adminAPIKey))
	{
		v1.GET("/auth/me", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"authenticated": true})
		})
		v1.GET("/stats", h.GetStats)

		v1.GET("/companies", h.ListCompanies)
		v1.POST("/companies", h.CreateCompany)
		v1.PATCH("/companies/:id", h.UpdateCompany)
		v1.PUT("/companies/:id", h.UpdateCompany)

		v1.GET("/accounts", h.ListAccounts)
		v1.POST("/accounts", h.CreateAccount)
		v1.PATCH("/accounts/:id", h.UpdateAccount)
		v1.PUT("/accounts/:id", h.UpdateAccount)

		v1.GET("/routes", h.ListRoutes)
		v1.POST("/routes", h.CreateRoute)
		v1.PATCH("/routes/:id", h.UpdateRoute)
		v1.PUT("/routes/:id", h.UpdateRoute)
		v1.DELETE("/routes/:id", h.DeleteRoute)

		v1.GET("/events", h.ListEvents)
		v1.GET("/events/:id", h.GetEvent)
		v1.POST("/events/:id/route", h.RouteUnroutedEvent)

		v1.GET("/deliveries", h.ListDeliveries)
		v1.POST("/deliveries/:id/retry", h.RetryDelivery)
	}

	return r
}
