package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/abdelrahmantarek/go-url-shortener/internal/cache"
	"github.com/abdelrahmantarek/go-url-shortener/internal/middleware"
	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func newRedisLimiter(t *testing.T, rps int) (*middleware.RedisRateLimiter, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	c, err := cache.New(mr.Addr())
	require.NoError(t, err)
	t.Cleanup(func() { c.Close() })
	return middleware.NewRedisRateLimiter(c, rps), mr
}

func doGet(t *testing.T, h http.Handler) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w.Code
}

func doGetPath(t *testing.T, h http.Handler, path string) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = "127.0.0.1:1234"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w.Code
}

func TestRedisRateLimiter_AllowsUnderLimit(t *testing.T) {
	rl, _ := newRedisLimiter(t, 3)
	h := rl.Middleware(okHandler())

	for i := 0; i < 3; i++ {
		assert.Equal(t, http.StatusOK, doGet(t, h), "request %d should be allowed", i+1)
	}
}

func TestRedisRateLimiter_BlocksAtLimit(t *testing.T) {
	rl, _ := newRedisLimiter(t, 2)
	h := rl.Middleware(okHandler())

	assert.Equal(t, http.StatusOK, doGet(t, h))
	assert.Equal(t, http.StatusOK, doGet(t, h))
	assert.Equal(t, http.StatusTooManyRequests, doGet(t, h))
}

func TestRedisRateLimiter_ResetsAfterWindow(t *testing.T) {
	rl, mr := newRedisLimiter(t, 1)
	h := rl.Middleware(okHandler())

	assert.Equal(t, http.StatusOK, doGet(t, h))           // uses the 1 allowed slot
	assert.Equal(t, http.StatusTooManyRequests, doGet(t, h)) // blocked

	mr.FastForward(time.Second) // expire the window key

	assert.Equal(t, http.StatusOK, doGet(t, h), "should be allowed after window resets")
}

func TestRedisRateLimiter_RedisFailure_AllowsRequest(t *testing.T) {
	mr := miniredis.RunT(t)
	c, err := cache.New(mr.Addr())
	require.NoError(t, err)
	defer c.Close()

	rl := middleware.NewRedisRateLimiter(c, 1)
	h := rl.Middleware(okHandler())

	mr.Close() // kill Redis mid-flight

	// request must be allowed (fallback), not crash
	assert.Equal(t, http.StatusOK, doGet(t, h))
}

func TestRedisRateLimiter_RetryAfterHeader(t *testing.T) {
	rl, _ := newRedisLimiter(t, 1)
	h := rl.Middleware(okHandler())

	doGet(t, h) // consume the slot

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.Equal(t, "1", w.Header().Get("Retry-After"))
}

func TestRateLimiter_BypassesSwaggerPath(t *testing.T) {
	rl := middleware.NewRateLimiter(1, 1)
	h := rl.Middleware(okHandler())

	for i := 0; i < 10; i++ {
		assert.Equal(t, http.StatusOK, doGetPath(t, h, "/swagger/doc.json"))
	}
}

func TestRedisRateLimiter_BypassesSwaggerPath(t *testing.T) {
	rl, _ := newRedisLimiter(t, 1)
	h := rl.Middleware(okHandler())

	for i := 0; i < 10; i++ {
		assert.Equal(t, http.StatusOK, doGetPath(t, h, "/swagger/doc.json"))
	}
}
