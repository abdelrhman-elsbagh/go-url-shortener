package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/abdelrahmantarek/go-url-shortener/internal/handler"
	"github.com/abdelrahmantarek/go-url-shortener/internal/middleware"
	"github.com/abdelrahmantarek/go-url-shortener/internal/model"
	"github.com/abdelrahmantarek/go-url-shortener/internal/service"
	"github.com/abdelrahmantarek/go-url-shortener/internal/storage"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	gin.SetMode(gin.TestMode)
}

const migrationSQL = `
CREATE TABLE IF NOT EXISTS urls (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    short_code   TEXT     NOT NULL UNIQUE,
    original_url TEXT     NOT NULL,
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at   DATETIME,
    click_count  INTEGER  NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_short_code   ON urls(short_code);
CREATE INDEX        IF NOT EXISTS idx_original_url ON urls(original_url);
`

type testEnv struct {
	server *httptest.Server
	store  *storage.SQLiteStorage
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	f, err := os.CreateTemp("", "handler-test-*.db")
	require.NoError(t, err)
	f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })

	store, err := storage.New(f.Name(), migrationSQL)
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	svc := service.New(store, "http://localhost", logger)
	rl := middleware.NewRateLimiter(10, 20)

	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.SecurityHeaders)
	r.Use(middleware.RequestID)
	r.Use(middleware.AdaptLimiter(rl))
	r.GET("/health", handler.HealthHandler("1.0.0"))
	r.GET("/swagger/index.html", func(c *gin.Context) { c.Status(http.StatusOK) })
	r.POST("/api/v1/encode", handler.NewEncodeHandler(svc, svc.FullShortURL, logger).Handle)
	r.POST("/api/v1/decode", handler.NewDecodeHandler(svc, logger).Handle)

	ts := httptest.NewServer(r)
	t.Cleanup(ts.Close)
	return &testEnv{server: ts, store: store}
}

func postJSON(t *testing.T, ts *httptest.Server, path string, body any) *http.Response {
	t.Helper()
	b, err := json.Marshal(body)
	require.NoError(t, err)
	resp, err := ts.Client().Post(ts.URL+path, "application/json", bytes.NewReader(b))
	require.NoError(t, err)
	return resp
}

// ---- GET /health ------------------------------------------------------------

func TestHealth(t *testing.T) {
	env := newTestEnv(t)
	resp, err := env.server.Client().Get(env.server.URL + "/health")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "ok", body["status"])
	assert.Equal(t, "1.0.0", body["version"])
}

func TestSecurityHeaders(t *testing.T) {
	env := newTestEnv(t)
	resp, err := env.server.Client().Get(env.server.URL + "/health")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, "nosniff", resp.Header.Get("X-Content-Type-Options"))
	assert.Equal(t, "DENY", resp.Header.Get("X-Frame-Options"))
	assert.Equal(t, "1; mode=block", resp.Header.Get("X-XSS-Protection"))
	assert.Equal(t, "strict-origin-when-cross-origin", resp.Header.Get("Referrer-Policy"))
	assert.Equal(t, "default-src 'none'", resp.Header.Get("Content-Security-Policy"))
}

func TestSecurityHeaders_SwaggerCSP(t *testing.T) {
	env := newTestEnv(t)
	resp, err := env.server.Client().Get(env.server.URL + "/swagger/index.html")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'", resp.Header.Get("Content-Security-Policy"))
}

func TestRequestID_Present(t *testing.T) {
	env := newTestEnv(t)
	resp, err := env.server.Client().Get(env.server.URL + "/health")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.NotEmpty(t, resp.Header.Get("X-Request-ID"))
}

func TestRequestID_PassThrough(t *testing.T) {
	env := newTestEnv(t)
	req, err := http.NewRequest(http.MethodGet, env.server.URL+"/health", nil)
	require.NoError(t, err)
	req.Header.Set("X-Request-ID", "my-trace-id")

	resp, err := env.server.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, "my-trace-id", resp.Header.Get("X-Request-ID"))
}

// ---- POST /api/v1/encode ----------------------------------------------------

func TestEncode_ValidURL(t *testing.T) {
	env := newTestEnv(t)
	resp := postJSON(t, env.server, "/api/v1/encode", map[string]string{"url": "https://www.google.com"})
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.NotEmpty(t, body["short_code"])
	assert.Equal(t, "https://www.google.com", body["original_url"])
	assert.NotEmpty(t, body["short_url"])
}

func TestEncode_InvalidURL(t *testing.T) {
	env := newTestEnv(t)
	resp := postJSON(t, env.server, "/api/v1/encode", map[string]string{"url": "not-a-url"})
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestEncode_PrivateIP(t *testing.T) {
	env := newTestEnv(t)
	resp := postJSON(t, env.server, "/api/v1/encode", map[string]string{"url": "http://192.168.1.1/secret"})
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

func TestEncode_SameURLTwice(t *testing.T) {
	env := newTestEnv(t)

	r1 := postJSON(t, env.server, "/api/v1/encode", map[string]string{"url": "https://www.google.com"})
	defer r1.Body.Close()
	var b1 map[string]any
	require.NoError(t, json.NewDecoder(r1.Body).Decode(&b1))

	r2 := postJSON(t, env.server, "/api/v1/encode", map[string]string{"url": "https://www.google.com"})
	defer r2.Body.Close()
	var b2 map[string]any
	require.NoError(t, json.NewDecoder(r2.Body).Decode(&b2))

	assert.Equal(t, b1["short_code"], b2["short_code"], "same URL must produce same short code")
}

func TestEncode_JavascriptScheme(t *testing.T) {
	env := newTestEnv(t)
	resp := postJSON(t, env.server, "/api/v1/encode", map[string]string{"url": "javascript:alert(1)"})
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestEncode_DataScheme(t *testing.T) {
	env := newTestEnv(t)
	resp := postJSON(t, env.server, "/api/v1/encode", map[string]string{"url": "data:text/html,<h1>hi</h1>"})
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestEncode_MissingURL(t *testing.T) {
	env := newTestEnv(t)
	resp := postJSON(t, env.server, "/api/v1/encode", map[string]string{})
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

// ---- POST /api/v1/decode ----------------------------------------------------

func TestDecode_ValidCode(t *testing.T) {
	env := newTestEnv(t)

	er := postJSON(t, env.server, "/api/v1/encode", map[string]string{"url": "https://www.google.com"})
	defer er.Body.Close()
	var eb map[string]any
	require.NoError(t, json.NewDecoder(er.Body).Decode(&eb))
	code := eb["short_code"].(string)

	dr := postJSON(t, env.server, "/api/v1/decode", map[string]string{"short_code": code})
	defer dr.Body.Close()
	assert.Equal(t, http.StatusOK, dr.StatusCode)

	var db map[string]any
	require.NoError(t, json.NewDecoder(dr.Body).Decode(&db))
	assert.Equal(t, "https://www.google.com", db["original_url"])
}

func TestDecode_UnknownCode(t *testing.T) {
	env := newTestEnv(t)
	resp := postJSON(t, env.server, "/api/v1/decode", map[string]string{"short_code": "xxxxxx"})
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestDecode_ExpiredCode(t *testing.T) {
	env := newTestEnv(t)

	past := time.Now().Add(-time.Hour)
	u := &model.URL{
		ShortCode:   "expXXX",
		OriginalURL: "https://www.google.com",
		CreatedAt:   time.Now().Add(-2 * time.Hour),
		ExpiresAt:   &past,
	}
	require.NoError(t, env.store.Save(context.Background(), u))

	resp := postJSON(t, env.server, "/api/v1/decode", map[string]string{"short_code": "expXXX"})
	defer resp.Body.Close()
	assert.Equal(t, http.StatusGone, resp.StatusCode)
}

func TestDecode_MissingCode(t *testing.T) {
	env := newTestEnv(t)
	resp := postJSON(t, env.server, "/api/v1/decode", map[string]string{})
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

// ---- Rate limiting ----------------------------------------------------------

func TestRateLimit_429(t *testing.T) {
	rl := middleware.NewRateLimiter(1, 1)
	r := gin.New()
	r.Use(middleware.AdaptLimiter(rl))
	r.GET("/health", handler.HealthHandler("test"))

	ts := httptest.NewServer(r)
	defer ts.Close()

	// exhaust the burst
	resp, err := ts.Client().Get(ts.URL + "/health")
	require.NoError(t, err)
	resp.Body.Close()

	got429 := false
	for i := 0; i < 20; i++ {
		resp, err = ts.Client().Get(ts.URL + "/health")
		require.NoError(t, err)
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests {
			assert.NotEmpty(t, resp.Header.Get("Retry-After"))
			got429 = true
			break
		}
	}
	assert.True(t, got429, "expected 429 under tight rate limiting")
}
