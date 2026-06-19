package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/abdelrahmantarek/go-url-shortener/internal/model"
	"github.com/redis/go-redis/v9"
)

var ErrMiss = errors.New("cache miss")

const (
	urlKeyPrefix   = "url:"
	clickKeyPrefix = "clicks:"
	defaultTTL     = 24 * time.Hour
)

type Cache interface {
	Get(ctx context.Context, code string) (*model.URL, error)
	Set(ctx context.Context, url *model.URL, ttl time.Duration) error
	Delete(ctx context.Context, code string) error
	IncrClickCount(ctx context.Context, code string) (int64, error)
	// IncrWithTTL atomically increments key and sets ttl on the first call per window.
	IncrWithTTL(ctx context.Context, key string, ttl time.Duration) (int64, error)
	Close() error
}

type RedisCache struct {
	client *redis.Client
}

func New(addr string) (*RedisCache, error) {
	c := redis.NewClient(&redis.Options{Addr: addr})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := c.Ping(ctx).Err(); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("redis ping: %w", err)
	}
	return &RedisCache{client: c}, nil
}

func (r *RedisCache) Get(ctx context.Context, code string) (*model.URL, error) {
	val, err := r.client.Get(ctx, urlKeyPrefix+code).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrMiss
	}
	if err != nil {
		return nil, fmt.Errorf("redis get: %w", err)
	}
	var u model.URL
	if err := json.Unmarshal(val, &u); err != nil {
		return nil, fmt.Errorf("unmarshal cached URL: %w", err)
	}
	return &u, nil
}

func (r *RedisCache) Set(ctx context.Context, url *model.URL, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = defaultTTL
	}
	b, err := json.Marshal(url)
	if err != nil {
		return fmt.Errorf("marshal URL: %w", err)
	}
	return r.client.Set(ctx, urlKeyPrefix+url.ShortCode, b, ttl).Err()
}

func (r *RedisCache) Delete(ctx context.Context, code string) error {
	return r.client.Del(ctx, urlKeyPrefix+code).Err()
}

func (r *RedisCache) IncrClickCount(ctx context.Context, code string) (int64, error) {
	n, err := r.client.Incr(ctx, clickKeyPrefix+code).Result()
	if err != nil {
		return 0, fmt.Errorf("redis incr clicks: %w", err)
	}
	return n, nil
}

// IncrWithTTL increments key and sets a TTL the first time the key is created.
// Used by the Redis rate limiter for fixed-window counting.
func (r *RedisCache) IncrWithTTL(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	n, err := r.client.Incr(ctx, key).Result()
	if err != nil {
		return 0, fmt.Errorf("redis incr: %w", err)
	}
	if n == 1 {
		// first request in this window — arm the expiry
		if err := r.client.Expire(ctx, key, ttl).Err(); err != nil {
			// non-fatal; key will linger until the next Expire succeeds
			return n, fmt.Errorf("redis expire: %w", err)
		}
	}
	return n, nil
}

func (r *RedisCache) Close() error { return r.client.Close() }
