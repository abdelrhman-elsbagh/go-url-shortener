package cache_test

import (
	"context"
	"testing"
	"time"

	"github.com/abdelrahmantarek/go-url-shortener/internal/cache"
	"github.com/abdelrahmantarek/go-url-shortener/internal/model"
	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestCache(t *testing.T) (*cache.RedisCache, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	c, err := cache.New(mr.Addr())
	require.NoError(t, err)
	t.Cleanup(func() { c.Close() })
	return c, mr
}

func TestSet_AndGet(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()

	u := &model.URL{ShortCode: "abc123", OriginalURL: "https://example.com", ClickCount: 5}
	require.NoError(t, c.Set(ctx, u, time.Minute))

	got, err := c.Get(ctx, "abc123")
	require.NoError(t, err)
	assert.Equal(t, u.OriginalURL, got.OriginalURL)
	assert.Equal(t, u.ClickCount, got.ClickCount)
}

func TestGet_Miss(t *testing.T) {
	c, _ := newTestCache(t)
	_, err := c.Get(context.Background(), "doesnotexist")
	assert.ErrorIs(t, err, cache.ErrMiss)
}

func TestSet_Expiry(t *testing.T) {
	c, mr := newTestCache(t)

	u := &model.URL{ShortCode: "ttl1", OriginalURL: "https://example.com"}
	require.NoError(t, c.Set(context.Background(), u, time.Second))

	mr.FastForward(2 * time.Second)

	_, err := c.Get(context.Background(), "ttl1")
	assert.ErrorIs(t, err, cache.ErrMiss)
}

func TestDelete(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()

	u := &model.URL{ShortCode: "del1", OriginalURL: "https://example.com"}
	require.NoError(t, c.Set(ctx, u, time.Minute))
	require.NoError(t, c.Delete(ctx, "del1"))

	_, err := c.Get(ctx, "del1")
	assert.ErrorIs(t, err, cache.ErrMiss)
}

func TestIncrClickCount(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()

	n1, err := c.IncrClickCount(ctx, "code1")
	require.NoError(t, err)
	assert.Equal(t, int64(1), n1)

	n2, err := c.IncrClickCount(ctx, "code1")
	require.NoError(t, err)
	assert.Equal(t, int64(2), n2)
}

func TestIncrWithTTL_CountsAndExpires(t *testing.T) {
	c, mr := newTestCache(t)
	ctx := context.Background()

	n1, err := c.IncrWithTTL(ctx, "win:127.0.0.1", time.Second)
	require.NoError(t, err)
	assert.Equal(t, int64(1), n1)

	n2, err := c.IncrWithTTL(ctx, "win:127.0.0.1", time.Second)
	require.NoError(t, err)
	assert.Equal(t, int64(2), n2)

	// advance past the window — key should expire
	mr.FastForward(2 * time.Second)

	n3, err := c.IncrWithTTL(ctx, "win:127.0.0.1", time.Second)
	require.NoError(t, err)
	assert.Equal(t, int64(1), n3, "counter must reset after window expires")
}

func TestIncrClickCount_Independent(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()

	_, _ = c.IncrClickCount(ctx, "a")
	_, _ = c.IncrClickCount(ctx, "a")
	n, err := c.IncrClickCount(ctx, "b")
	require.NoError(t, err)
	assert.Equal(t, int64(1), n, "different codes should have independent counters")
}
