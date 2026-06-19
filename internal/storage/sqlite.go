package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/abdelrahmantarek/go-url-shortener/internal/model"
	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("record not found")

type Storage interface {
	Save(ctx context.Context, url *model.URL) error
	FindByCode(ctx context.Context, code string) (*model.URL, error)
	FindByOriginalURL(ctx context.Context, originalURL string) (*model.URL, error)
	IncrementClickCount(ctx context.Context, code string) error
	DeleteExpired(ctx context.Context) error
	Close() error
}

type SQLiteStorage struct {
	db *sql.DB
}

func New(dbPath string, schema string) (*SQLiteStorage, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// single writer — SQLite doesn't like concurrent writes
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	if schema != "" {
		if _, err = db.Exec(schema); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("run migrations: %w", err)
		}
	}

	return &SQLiteStorage{db: db}, nil
}

// DB exposes the underlying *sql.DB for use by the migrate package.
func (s *SQLiteStorage) DB() *sql.DB { return s.db }

func (s *SQLiteStorage) Save(ctx context.Context, url *model.URL) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	res, err := s.db.ExecContext(ctx,
		`INSERT INTO urls (short_code, original_url, created_at, expires_at, click_count)
		 VALUES (?, ?, ?, ?, ?)`,
		url.ShortCode,
		url.OriginalURL,
		url.CreatedAt.UTC(),
		nullTime(url.ExpiresAt),
		url.ClickCount,
	)
	if err != nil {
		return fmt.Errorf("insert url: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("last insert id: %w", err)
	}
	url.ID = id
	return nil
}

func (s *SQLiteStorage) FindByCode(ctx context.Context, code string) (*model.URL, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	row := s.db.QueryRowContext(ctx,
		`SELECT id, short_code, original_url, created_at, expires_at, click_count
		 FROM urls WHERE short_code = ? LIMIT 1`, code)
	return scanURL(row)
}

func (s *SQLiteStorage) FindByOriginalURL(ctx context.Context, originalURL string) (*model.URL, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	row := s.db.QueryRowContext(ctx,
		`SELECT id, short_code, original_url, created_at, expires_at, click_count
		 FROM urls WHERE original_url = ? LIMIT 1`, originalURL)
	return scanURL(row)
}

func (s *SQLiteStorage) IncrementClickCount(ctx context.Context, code string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := s.db.ExecContext(ctx,
		`UPDATE urls SET click_count = click_count + 1 WHERE short_code = ?`, code)
	return err
}

func (s *SQLiteStorage) DeleteExpired(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := s.db.ExecContext(ctx,
		`DELETE FROM urls WHERE expires_at IS NOT NULL AND expires_at <= ?`,
		time.Now().UTC())
	return err
}

func (s *SQLiteStorage) Close() error { return s.db.Close() }

func scanURL(row *sql.Row) (*model.URL, error) {
	var u model.URL
	var exp sql.NullTime
	var createdAt time.Time

	if err := row.Scan(&u.ID, &u.ShortCode, &u.OriginalURL, &createdAt, &exp, &u.ClickCount); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scan url: %w", err)
	}

	u.CreatedAt = createdAt.UTC()
	if exp.Valid {
		t := exp.Time.UTC()
		u.ExpiresAt = &t
	}
	return &u, nil
}

func nullTime(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: t.UTC(), Valid: true}
}
