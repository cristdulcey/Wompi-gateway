package http

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	// AdminSessionCookieName is the HttpOnly cookie name used by the Admin Console.
	AdminSessionCookieName = "wompi_admin_session"
	adminSessionTTL        = 12 * time.Hour
)

func signAdminSession(adminAPIKey string, expiresAtUnix int64) string {
	mac := hmac.New(sha256.New, []byte(adminAPIKey))
	_, _ = fmt.Fprintf(mac, "wompi-admin-session|%d", expiresAtUnix)
	sig := hex.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("%d.%s", expiresAtUnix, sig)
}

func verifyAdminSession(cookieValue string, adminAPIKey string, now time.Time) bool {
	if cookieValue == "" || adminAPIKey == "" {
		return false
	}
	parts := strings.SplitN(cookieValue, ".", 2)
	if len(parts) != 2 {
		return false
	}
	expiresAt, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || now.Unix() > expiresAt {
		return false
	}
	expected := signAdminSession(adminAPIKey, expiresAt)
	return hmac.Equal([]byte(cookieValue), []byte(expected))
}

func isRequestSecure(c *gin.Context) bool {
	if c.Request.TLS != nil {
		return true
	}
	return strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https")
}

// AdminLoginHandler authenticates the admin API key and issues an HttpOnly, SameSite=Strict session cookie.
func AdminLoginHandler(adminAPIKey string) gin.HandlerFunc {
	expectedBytes := []byte(adminAPIKey)

	return func(c *gin.Context) {
		var req struct {
			APIKey string `json:"api_key"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
			return
		}

		provided := strings.TrimSpace(req.APIKey)
		if provided == "" || !hmac.Equal([]byte(provided), expectedBytes) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid admin API key"})
			return
		}

		expiresAt := time.Now().UTC().Add(adminSessionTTL).Unix()
		token := signAdminSession(adminAPIKey, expiresAt)

		c.SetSameSite(http.SameSiteStrictMode)
		c.SetCookie(
			AdminSessionCookieName,
			token,
			int(adminSessionTTL.Seconds()),
			"/",
			"",
			isRequestSecure(c),
			true, // HttpOnly: inaccessible to document.cookie / XSS
		)

		c.JSON(http.StatusOK, gin.H{
			"authenticated": true,
			"expires_at":    expiresAt,
		})
	}
}

// AdminLogoutHandler clears the HttpOnly session cookie.
func AdminLogoutHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.SetSameSite(http.SameSiteStrictMode)
		c.SetCookie(
			AdminSessionCookieName,
			"",
			-1,
			"/",
			"",
			isRequestSecure(c),
			true,
		)
		c.JSON(http.StatusOK, gin.H{"authenticated": false})
	}
}

// AdminAuthMiddleware protects admin API endpoints using an HttpOnly session cookie,
// X-API-Key header, or Authorization: Bearer <token>.
func AdminAuthMiddleware(adminAPIKey string) gin.HandlerFunc {
	expectedBytes := []byte(adminAPIKey)

	return func(c *gin.Context) {
		// 1. Check HttpOnly session cookie (used by the browser Admin Console)
		if cookieVal, err := c.Cookie(AdminSessionCookieName); err == nil {
			if verifyAdminSession(cookieVal, adminAPIKey, time.Now().UTC()) {
				c.Next()
				return
			}
		}

		// 2. Fallback to X-API-Key or Bearer token (used by CLI / service integrations)
		provided := strings.TrimSpace(c.GetHeader("X-API-Key"))
		if provided == "" {
			authHeader := strings.TrimSpace(c.GetHeader("Authorization"))
			if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
				provided = strings.TrimSpace(authHeader[7:])
			}
		}

		if provided == "" || !hmac.Equal([]byte(provided), expectedBytes) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "unauthorized: valid session cookie, X-API-Key, or Bearer token required",
			})
			return
		}

		c.Next()
	}
}
