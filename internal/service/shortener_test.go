package service_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/abdelrahmantarek/go-url-shortener/internal/model"
	"github.com/abdelrahmantarek/go-url-shortener/internal/service"
	"github.com/abdelrahmantarek/go-url-shortener/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"log/slog"
)

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

func newTestService(t *testing.T) *service.ShortenerService {
	t.Helper()
	f, err := os.CreateTemp("", "svc-test-*.db")
	require.NoError(t, err)
	f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })

	s, err := storage.New(f.Name(), migrationSQL)
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	return service.New(s, "http://localhost:8080", logger)
}

func TestEncode_ValidURL(t *testing.T) {
	svc := newTestService(t)
	record, err := svc.Encode(context.Background(), "https://www.google.com")
	require.NoError(t, err)
	assert.NotEmpty(t, record.ShortCode)
	assert.Equal(t, "https://www.google.com", record.OriginalURL)
}

func TestEncode_Idempotent(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	r1, err := svc.Encode(ctx, "https://www.google.com")
	require.NoError(t, err)

	r2, err := svc.Encode(ctx, "https://www.google.com")
	require.NoError(t, err)

	assert.Equal(t, r1.ShortCode, r2.ShortCode, "same URL must produce same short code")
}

func TestEncode_InvalidScheme(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.Encode(context.Background(), "ftp://example.com")
	assert.True(t, errors.Is(err, service.ErrInvalidURL))
}

func TestEncode_EmptyURL(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.Encode(context.Background(), "")
	assert.True(t, errors.Is(err, service.ErrInvalidURL))
}

func TestEncode_PrivateIP(t *testing.T) {
	svc := newTestService(t)
	// localhost resolves to 127.0.0.1 which is blocked.
	_, err := svc.Encode(context.Background(), "http://localhost/path")
	assert.True(t, errors.Is(err, service.ErrInvalidURL))
}

func TestEncode_TooLong(t *testing.T) {
	svc := newTestService(t)
	long := "https://example.com/" + string(make([]byte, 2048))
	_, err := svc.Encode(context.Background(), long)
	assert.True(t, errors.Is(err, service.ErrInvalidURL))
}

func TestDecode_ValidCode(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	encoded, err := svc.Encode(ctx, "https://www.google.com")
	require.NoError(t, err)

	// Small sleep so background goroutine doesn't interfere with test timing.
	time.Sleep(50 * time.Millisecond)

	decoded, err := svc.Decode(ctx, encoded.ShortCode)
	require.NoError(t, err)
	assert.Equal(t, "https://www.google.com", decoded.OriginalURL)
}

func TestDecode_NotFound(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.Decode(context.Background(), "xxxxxx")
	assert.True(t, errors.Is(err, service.ErrURLNotFound))
}

func TestDecode_Expired(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	encoded, err := svc.Encode(ctx, "https://www.google.com")
	require.NoError(t, err)

	// Manually expire the record by re-saving via storage.
	f, err2 := os.CreateTemp("", "svc-expire-*.db")
	require.NoError(t, err2)
	f.Close()
	defer os.Remove(f.Name())

	store2, err2 := storage.New(f.Name(), migrationSQL)
	require.NoError(t, err2)
	defer store2.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	svc2 := service.New(store2, "http://localhost:8080", logger)

	past := time.Now().Add(-time.Hour)
	expiredURL := &model.URL{
		ShortCode:   encoded.ShortCode,
		OriginalURL: "https://www.google.com",
		CreatedAt:   time.Now().Add(-2 * time.Hour),
		ExpiresAt:   &past,
	}
	require.NoError(t, store2.Save(ctx, expiredURL))

	_, err = svc2.Decode(ctx, encoded.ShortCode)
	assert.True(t, errors.Is(err, service.ErrURLExpired))
}

func TestFullShortURL(t *testing.T) {
	svc := newTestService(t)
	assert.Equal(t, "http://localhost:8080/abc123", svc.FullShortURL("abc123"))
}
