package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
)

func SecurityHeaders(c *gin.Context) {
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("X-Frame-Options", "DENY")
	c.Header("X-XSS-Protection", "1; mode=block")
	c.Header("Referrer-Policy", "strict-origin-when-cross-origin")

	// Swagger UI serves static scripts/styles from /swagger/* and needs a looser
	// policy than API JSON endpoints.
	if strings.HasPrefix(c.Request.URL.Path, "/swagger/") {
		c.Header("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'")
	} else {
		c.Header("Content-Security-Policy", "default-src 'none'")
	}

	c.Next()
}
