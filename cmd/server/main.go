package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	_ "github.com/abdelrahmantarek/go-url-shortener/docs"
	"github.com/abdelrahmantarek/go-url-shortener/internal/cache"
	"github.com/abdelrahmantarek/go-url-shortener/internal/handler"
	"github.com/abdelrahmantarek/go-url-shortener/internal/middleware"
	"github.com/abdelrahmantarek/go-url-shortener/internal/migrate"
	"github.com/abdelrahmantarek/go-url-shortener/internal/service"
	"github.com/abdelrahmantarek/go-url-shortener/internal/storage"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

const version = "1.0.0"

// @title           Go URL Shortener API
// @version         1.0.0
// @description     Production-ready URL shortening service with encode and decode endpoints.
// @BasePath        /
// @schemes         http https
func main() {
	_ = godotenv.Load()

	logger := buildLogger()

	cfg, err := loadConfig()
	if err != nil {
		logger.Error("load config", slog.String("err", err.Error()))
		os.Exit(1)
	}

	store, err := storage.New(cfg.dbPath, "")
	if err != nil {
		logger.Error("open storage", slog.String("err", err.Error()))
		os.Exit(1)
	}
	defer func() {
		if err := store.Close(); err != nil {
			logger.Error("close storage", slog.String("err", err.Error()))
		}
	}()

	if err := migrate.New(store.DB()).Run(os.DirFS("migrations")); err != nil {
		logger.Error("run migrations", slog.String("err", err.Error()))
		os.Exit(1)
	}

	// Redis is optional — failures degrade gracefully to DB-only / in-memory limiting
	var c cache.Cache
	if addr := envStr("REDIS_URL", ""); addr != "" {
		rc, err := cache.New(addr)
		if err != nil {
			logger.Warn("redis unavailable, cache disabled", slog.String("err", err.Error()))
		} else {
			logger.Info("redis connected", slog.String("addr", addr))
			c = rc
			defer rc.Close()
		}
	}

	var rl middleware.Limiter
	if c != nil {
		rl = middleware.NewRedisRateLimiter(c, int(cfg.rlRPS))
	} else {
		rl = middleware.NewRateLimiter(cfg.rlRPS, cfg.rlBurst)
	}

	svc := service.New(store, cfg.baseURL, logger).WithCache(c)

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.port),
		Handler:      buildRouter(svc, rl, logger),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	errc := make(chan error, 1)
	go func() {
		logger.Info("server starting", slog.String("addr", srv.Addr), slog.String("version", version))
		errc <- srv.ListenAndServe()
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err = <-errc:
		logger.Error("server error", slog.String("err", err.Error()))
	case sig := <-quit:
		logger.Info("shutting down", slog.String("signal", sig.String()))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err = srv.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown failed", slog.String("err", err.Error()))
		os.Exit(1)
	}
	logger.Info("bye")
}

func buildRouter(svc *service.ShortenerService, rl middleware.Limiter, logger *slog.Logger) http.Handler {
	if envStr("APP_ENV", "development") == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.SecurityHeaders)
	r.Use(middleware.RequestID)
	r.Use(middleware.RequestLogger(logger))
	r.Use(middleware.AdaptLimiter(rl))

	r.GET("/health", handler.HealthHandler(version))
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	r.POST("/api/v1/encode", handler.NewEncodeHandler(svc, svc.FullShortURL, logger).Handle)
	r.POST("/api/v1/decode", handler.NewDecodeHandler(svc, logger).Handle)

	return r
}

type config struct {
	port    int
	baseURL string
	dbPath  string
	rlRPS   float64
	rlBurst int
	env     string
}

func (c *config) validate() error {
	if c.port < 1 || c.port > 65535 {
		return fmt.Errorf("port must be 1-65535, got %d", c.port)
	}
	if c.dbPath == "" {
		return errors.New("DB_PATH must not be empty")
	}
	if c.rlRPS <= 0 {
		return errors.New("RATE_LIMIT_RPS must be > 0")
	}
	if float64(c.rlBurst) < c.rlRPS {
		return errors.New("RATE_LIMIT_BURST must be >= RATE_LIMIT_RPS")
	}
	return nil
}

func loadConfig() (*config, error) {
	port, err := envInt("APP_PORT", 8080)
	if err != nil {
		return nil, fmt.Errorf("APP_PORT: %w", err)
	}
	rps, err := envFloat("RATE_LIMIT_RPS", 10)
	if err != nil {
		return nil, fmt.Errorf("RATE_LIMIT_RPS: %w", err)
	}
	burst, err := envInt("RATE_LIMIT_BURST", 20)
	if err != nil {
		return nil, fmt.Errorf("RATE_LIMIT_BURST: %w", err)
	}

	cfg := &config{
		port:    port,
		baseURL: envStr("APP_BASE_URL", fmt.Sprintf("http://localhost:%d", port)),
		dbPath:  envStr("DB_PATH", "./data/urls.db"),
		rlRPS:   rps,
		rlBurst: burst,
		env:     envStr("APP_ENV", "development"),
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func buildLogger() *slog.Logger {
	env := envStr("APP_ENV", "development")

	level := slog.LevelInfo
	switch envStr("LOG_LEVEL", "info") {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}

	opts := &slog.HandlerOptions{Level: level}
	if env == "production" {
		return slog.New(slog.NewJSONHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, opts))
}

func envStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s: not a valid int (%q)", key, v)
	}
	return n, nil
}

func envFloat(key string, def float64) (float64, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: not a valid float (%q)", key, v)
	}
	return f, nil
}
