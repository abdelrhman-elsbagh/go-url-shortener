package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// AdaptLimiter wraps a Limiter (net/http middleware API) into a gin.HandlerFunc.
// The Limiter's Middleware method keeps its net/http signature so that the pure
// unit tests in ratelimit_test.go continue to work without any Gin dependency.
func AdaptLimiter(l Limiter) gin.HandlerFunc {
	return wrapHTTPMiddleware(l.Middleware)
}

// wrapHTTPMiddleware adapts any net/http middleware (func(Handler) Handler) to
// a gin.HandlerFunc. It bridges the two middleware styles by:
//   - passing Gin's response writer to the wrapped handler
//   - updating c.Request if the middleware replaces the request (e.g. context injection)
//   - calling c.Abort() when the middleware denies the request (never calls next)
func wrapHTTPMiddleware(m func(http.Handler) http.Handler) gin.HandlerFunc {
	return func(c *gin.Context) {
		var resumed bool
		m(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c.Request = r
			resumed = true
			c.Next()
		})).ServeHTTP(c.Writer, c.Request)
		if !resumed {
			c.Abort()
		}
	}
}
