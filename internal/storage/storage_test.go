package storage_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/abdelrahmantarek/go-url-shortener/internal/model"
	"github.com/abdelrahmantarek/go-url-shortener/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func newTestStorage(t *testing.T) *storage.SQLiteStorage {
	t.Helper()
	f, err := os.CreateTemp("", "url-shortener-test-*.db")
	require.NoError(t, err)
	f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })

	s, err := storage.New(f.Name(), migrationSQL)
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	return s
}

func TestSave_AndFindByCode(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	u := &model.URL{
		ShortCode:   "abc123",
		OriginalURL: "https://example.com",
		CreatedAt:   time.Now().UTC().Truncate(time.Second),
	}
	require.NoError(t, s.Save(ctx, u))
	assert.Greater(t, u.ID, int64(0))

	got, err := s.FindByCode(ctx, "abc123")
	require.NoError(t, err)
	assert.Equal(t, u.ShortCode, got.ShortCode)
	assert.Equal(t, u.OriginalURL, got.OriginalURL)
}

func TestFindByCode_NotFound(t *testing.T) {
	s := newTestStorage(t)
	_, err := s.FindByCode(context.Background(), "nope")
	assert.ErrorIs(t, err, storage.ErrNotFound)
}

func TestFindByOriginalURL(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	u := &model.URL{
		ShortCode:   "xyz789",
		OriginalURL: "https://example.com/original",
		CreatedAt:   time.Now().UTC(),
	}
	require.NoError(t, s.Save(ctx, u))

	got, err := s.FindByOriginalURL(ctx, "https://example.com/original")
	require.NoError(t, err)
	assert.Equal(t, "xyz789", got.ShortCode)
}

func TestFindByOriginalURL_NotFound(t *testing.T) {
	s := newTestStorage(t)
	_, err := s.FindByOriginalURL(context.Background(), "https://notexist.example")
	assert.ErrorIs(t, err, storage.ErrNotFound)
}

func TestIncrementClickCount(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	u := &model.URL{
		ShortCode:   "click1",
		OriginalURL: "https://example.com/click",
		CreatedAt:   time.Now().UTC(),
	}
	require.NoError(t, s.Save(ctx, u))

	require.NoError(t, s.IncrementClickCount(ctx, "click1"))
	require.NoError(t, s.IncrementClickCount(ctx, "click1"))

	got, err := s.FindByCode(ctx, "click1")
	require.NoError(t, err)
	assert.Equal(t, int64(2), got.ClickCount)
}

func TestDeleteExpired(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	past := time.Now().Add(-time.Hour).UTC()
	u := &model.URL{
		ShortCode:   "expired1",
		OriginalURL: "https://example.com/old",
		CreatedAt:   time.Now().UTC(),
		ExpiresAt:   &past,
	}
	require.NoError(t, s.Save(ctx, u))

	require.NoError(t, s.DeleteExpired(ctx))

	_, err := s.FindByCode(ctx, "expired1")
	assert.ErrorIs(t, err, storage.ErrNotFound)
}

func TestSave_DuplicateCode(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	u1 := &model.URL{ShortCode: "dup", OriginalURL: "https://a.com", CreatedAt: time.Now().UTC()}
	require.NoError(t, s.Save(ctx, u1))

	u2 := &model.URL{ShortCode: "dup", OriginalURL: "https://b.com", CreatedAt: time.Now().UTC()}
	assert.Error(t, s.Save(ctx, u2))
}
