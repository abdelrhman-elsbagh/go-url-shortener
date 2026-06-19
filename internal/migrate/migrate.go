package migrate

import (
	"database/sql"
	"fmt"
	"io/fs"
	"sort"
)

type Migrator struct {
	db *sql.DB
}

func New(db *sql.DB) *Migrator { return &Migrator{db: db} }

// Run applies any unapplied *.sql files from fsys in lexicographic order.
// Each migration and its schema_migrations record are committed in one transaction,
// so a partial failure leaves no trace and is safe to retry.
func (m *Migrator) Run(fsys fs.FS) error {
	if _, err := m.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		name       TEXT     PRIMARY KEY,
		applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		return fmt.Errorf("create migrations table: %w", err)
	}

	files, err := fs.Glob(fsys, "*.sql")
	if err != nil {
		return fmt.Errorf("glob migrations: %w", err)
	}
	sort.Strings(files)

	for _, name := range files {
		var n int
		if err := m.db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE name = ?`, name).Scan(&n); err != nil {
			return fmt.Errorf("check %s: %w", name, err)
		}
		if n > 0 {
			continue
		}

		body, err := fs.ReadFile(fsys, name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		if err := m.applyInTx(name, string(body)); err != nil {
			return err
		}
	}
	return nil
}

func (m *Migrator) applyInTx(name, body string) error {
	tx, err := m.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx for %s: %w", name, err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.Exec(body); err != nil {
		return fmt.Errorf("apply %s: %w", name, err)
	}
	if _, err := tx.Exec(`INSERT INTO schema_migrations (name) VALUES (?)`, name); err != nil {
		return fmt.Errorf("record %s: %w", name, err)
	}
	return tx.Commit()
}
