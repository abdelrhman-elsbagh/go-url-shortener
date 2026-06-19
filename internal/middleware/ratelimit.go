package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/abdelrahmantarek/go-url-shortener/internal/cache"
	"golang.org/x/time/rate"
)

// Limiter is the common interface for both in-memory and Redis-backed rate limiters.
type Limiter interface {
	Middleware(next http.Handler) http.Handler
}

type ipLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// RateLimiter is a per-IP token-bucket limiter. Stale entries are evicted
// in the background every minute.
type RateLimiter struct {
	mu      sync.Mutex
	clients map[string]*ipLimiter
	rps     rate.Limit
	burst   int
}

func NewRateLimiter(rps float64, burst int) *RateLimiter {
	rl := &RateLimiter{
		clients: make(map[string]*ipLimiter),
		rps:     rate.Limit(rps),
		burst:   burst,
	}
	go rl.cleanupLoop()
	return rl
}

func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !rl.getLimiter(realIP(r)).Allow() {
			retryAfter := int(1 / float64(rl.rps))
			if retryAfter < 1 {
				retryAfter = 1
			}
			w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"code":"RATE_LIMIT_EXCEEDED","message":"too many requests","details":"slow down and retry after the indicated period"}}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (rl *RateLimiter) getLimiter(ip string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	entry, ok := rl.clients[ip]
	if !ok {
		entry = &ipLimiter{limiter: rate.NewLimiter(rl.rps, rl.burst)}
		rl.clients[ip] = entry
	}
	entry.lastSeen = time.Now()
	return entry.limiter
}

func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		rl.mu.Lock()
		for ip, e := range rl.clients {
			if time.Since(e.lastSeen) > 3*time.Minute {
				delete(rl.clients, ip)
			}
		}
		rl.mu.Unlock()
	}
}

// realIP tries X-Forwarded-For and X-Real-IP before falling back to RemoteAddr.
func realIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		part := splitFirst(xff, ',')
		if ip := net.ParseIP(part); ip != nil {
			return ip.String()
		}
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		if ip := net.ParseIP(xri); ip != nil {
			return ip.String()
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func splitFirst(s string, sep rune) string {
	for i, ch := range s {
		if ch == sep {
			return trimSpace(s[:i])
		}
	}
	return trimSpace(s)
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}

// RedisRateLimiter is a fixed-window rate limiter backed by Redis.
// Window size = 1 second (matches the rps semantics of RateLimiter).
// On any Redis error it allows the request and logs a warning — never crashes.
type RedisRateLimiter struct {
	cache  cache.Cache
	rps    int
	window time.Duration
}

func NewRedisRateLimiter(client cache.Cache, rps int) *RedisRateLimiter {
	return &RedisRateLimiter{cache: client, rps: rps, window: time.Second}
}

func (r *RedisRateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ip := realIP(req)
		key := "rl:" + ip

		ctx, cancel := context.WithTimeout(req.Context(), 100*time.Millisecond)
		defer cancel()

		n, err := r.cache.IncrWithTTL(ctx, key, r.window)
		if err != nil {
			slog.Default().Warn("redis rate limiter error, allowing request",
				slog.String("ip", ip),
				slog.String("err", err.Error()),
			)
			next.ServeHTTP(w, req)
			return
		}

		if int(n) > r.rps {
			w.Header().Set("Retry-After", "1")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"code":"RATE_LIMIT_EXCEEDED","message":"too many requests","details":"slow down and retry after the indicated period"}}`))
			return
		}

		next.ServeHTTP(w, req)
	})
}
