package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/abdelrahmantarek/go-url-shortener/internal/cache"
	"github.com/abdelrahmantarek/go-url-shortener/internal/model"
	"github.com/abdelrahmantarek/go-url-shortener/internal/storage"
	"github.com/abdelrahmantarek/go-url-shortener/pkg/base62"
)

var (
	ErrInvalidURL  = errors.New("invalid URL")
	ErrURLNotFound = errors.New("URL not found")
	ErrURLExpired  = errors.New("URL expired")
	ErrMaxRetries  = errors.New("couldn't generate a unique code, too many collisions")
)

const maxRetries = 5

type Service interface {
	Encode(ctx context.Context, originalURL string) (*model.URL, error)
	Decode(ctx context.Context, shortCode string) (*model.URL, error)
}

type ShortenerService struct {
	store   storage.Storage
	cache   cache.Cache // nil = no cache
	baseURL string
	logger  *slog.Logger
}

func New(store storage.Storage, baseURL string, logger *slog.Logger) *ShortenerService {
	return &ShortenerService{
		store:   store,
		baseURL: strings.TrimRight(baseURL, "/"),
		logger:  logger,
	}
}

func (s *ShortenerService) WithCache(c cache.Cache) *ShortenerService {
	s.cache = c
	return s
}

func (s *ShortenerService) Encode(ctx context.Context, originalURL string) (*model.URL, error) {
	originalURL = strings.TrimSpace(originalURL)

	if err := validateURL(originalURL); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidURL, err.Error())
	}

	// already shortened — return existing record
	if existing, err := s.store.FindByOriginalURL(ctx, originalURL); err == nil {
		s.logger.InfoContext(ctx, "returning existing short code", slog.String("short_code", existing.ShortCode))
		return existing, nil
	} else if !errors.Is(err, storage.ErrNotFound) {
		return nil, fmt.Errorf("lookup original URL: %w", err)
	}

	// generate a collision-free code
	var code string
	for i := 0; i < maxRetries; i++ {
		c, err := base62.Generate()
		if err != nil {
			return nil, fmt.Errorf("generate code: %w", err)
		}
		if _, err = s.store.FindByCode(ctx, c); errors.Is(err, storage.ErrNotFound) {
			code = c
			break
		} else if err != nil {
			return nil, fmt.Errorf("collision check: %w", err)
		}
	}
	if code == "" {
		return nil, ErrMaxRetries
	}

	record := &model.URL{
		ShortCode:   code,
		OriginalURL: originalURL,
		CreatedAt:   time.Now().UTC(),
	}
	if err := s.store.Save(ctx, record); err != nil {
		return nil, fmt.Errorf("save URL: %w", err)
	}

	if s.cache != nil {
		if err := s.cache.Set(ctx, record, 24*time.Hour); err != nil {
			s.logger.Warn("cache set failed", slog.String("err", err.Error()))
		}
	}

	s.logger.InfoContext(ctx, "encoded URL", slog.String("short_code", code))
	return record, nil
}

func (s *ShortenerService) Decode(ctx context.Context, shortCode string) (*model.URL, error) {
	shortCode = strings.TrimSpace(shortCode)
	if shortCode == "" {
		return nil, fmt.Errorf("%w: short code is empty", ErrInvalidURL)
	}

	// try cache first; degrade gracefully on any error
	if s.cache != nil {
		if u, err := s.cache.Get(ctx, shortCode); err == nil {
			s.logger.InfoContext(ctx, "cache hit", slog.String("short_code", shortCode))
			return u, nil
		} else if !errors.Is(err, cache.ErrMiss) {
			s.logger.Warn("cache get error", slog.String("err", err.Error()))
		}
	}

	record, err := s.store.FindByCode(ctx, shortCode)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, ErrURLNotFound
		}
		return nil, fmt.Errorf("find by code: %w", err)
	}

	if record.IsExpired() {
		return nil, ErrURLExpired
	}

	// fire-and-forget: increment DB counter + invalidate cache so next read is fresh
	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.store.IncrementClickCount(bgCtx, shortCode); err != nil {
			s.logger.Warn("increment click count failed",
				slog.String("short_code", shortCode),
				slog.String("err", err.Error()),
			)
		}
		if s.cache != nil {
			_ = s.cache.Delete(bgCtx, shortCode)
		}
	}()

	if s.cache != nil {
		if err := s.cache.Set(ctx, record, 24*time.Hour); err != nil {
			s.logger.Warn("cache set failed", slog.String("err", err.Error()))
		}
	}

	s.logger.InfoContext(ctx, "decoded short code", slog.String("short_code", shortCode))
	return record, nil
}

func (s *ShortenerService) FullShortURL(code string) string {
	return s.baseURL + "/" + code
}

func validateURL(raw string) error {
	if len(raw) > 2048 {
		return errors.New("URL too long (max 2048 chars)")
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("malformed URL: %w", err)
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("only http/https allowed, got %q", parsed.Scheme)
	}

	host := parsed.Hostname()
	if host == "" {
		return errors.New("URL has no hostname")
	}

	lh := strings.ToLower(host)
	if lh == "localhost" || strings.HasSuffix(lh, ".localhost") {
		return errors.New("localhost not allowed")
	}

	// resolve and block private ranges (SSRF prevention)
	addrs, err := net.LookupHost(host)
	if err != nil {
		return fmt.Errorf("couldn't resolve %q: %w", host, err)
	}
	for _, addr := range addrs {
		ip := net.ParseIP(addr)
		if ip == nil {
			continue
		}
		if isPrivateIP(ip) {
			return fmt.Errorf("resolves to private IP (%s)", addr)
		}
	}

	return nil
}

var privateRanges = func() []*net.IPNet {
	cidrs := []string{
		"127.0.0.0/8",
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"169.254.0.0/16",
		"::1/128",
		"fc00::/7",
		"fe80::/10",
	}
	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		_, n, _ := net.ParseCIDR(cidr)
		nets = append(nets, n)
	}
	return nets
}()

func isPrivateIP(ip net.IP) bool {
	for _, n := range privateRanges {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}
