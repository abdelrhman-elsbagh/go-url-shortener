package migrate_test

import (
	"database/sql"
	"os"
	"testing"
	"testing/fstest"

	"github.com/abdelrahmantarek/go-url-shortener/internal/migrate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func newDB(t *testing.T) *sql.DB {
	t.Helper()
	f, err := os.CreateTemp("", "migrate-test-*.db")
	require.NoError(t, err)
	f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })

	db, err := sql.Open("sqlite", f.Name())
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

func TestRun_AppliesInOrder(t *testing.T) {
	db := newDB(t)
	m := migrate.New(db)

	fsys := fstest.MapFS{
		"001_create.sql": {Data: []byte(`CREATE TABLE foo (id INTEGER PRIMARY KEY);`)},
		"002_alter.sql":  {Data: []byte(`ALTER TABLE foo ADD COLUMN name TEXT;`)},
	}

	require.NoError(t, m.Run(fsys))

	_, err := db.Exec(`INSERT INTO foo (name) VALUES ('hello')`)
	assert.NoError(t, err, "column from second migration should exist")
}

func TestRun_Idempotent(t *testing.T) {
	db := newDB(t)
	m := migrate.New(db)

	fsys := fstest.MapFS{
		"001_init.sql": {Data: []byte(`CREATE TABLE bar (id INTEGER PRIMARY KEY);`)},
	}

	require.NoError(t, m.Run(fsys))
	require.NoError(t, m.Run(fsys), "second run should be a no-op")
}

func TestRun_TracksApplied(t *testing.T) {
	db := newDB(t)
	m := migrate.New(db)

	fsys := fstest.MapFS{
		"001_a.sql": {Data: []byte(`CREATE TABLE t1 (id INTEGER PRIMARY KEY);`)},
		"002_b.sql": {Data: []byte(`CREATE TABLE t2 (id INTEGER PRIMARY KEY);`)},
	}

	require.NoError(t, m.Run(fsys))

	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&count))
	assert.Equal(t, 2, count)
}

func TestRun_TransactionRollback(t *testing.T) {
	db := newDB(t)

	// pre-create schema_migrations so we can attach a trigger to it
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		name TEXT PRIMARY KEY,
		applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`)
	require.NoError(t, err)

	// this trigger will make every INSERT into schema_migrations fail
	_, err = db.Exec(`
		CREATE TRIGGER block_migrations BEFORE INSERT ON schema_migrations
		BEGIN
			SELECT RAISE(ABORT, 'simulated failure');
		END`)
	require.NoError(t, err)

	m := migrate.New(db)
	fsys := fstest.MapFS{
		"001_tx.sql": {Data: []byte(`CREATE TABLE tx_test (id INTEGER PRIMARY KEY);`)},
	}

	err = m.Run(fsys)
	assert.Error(t, err, "run should fail when schema_migrations INSERT is blocked")

	// the migration SQL (CREATE TABLE tx_test) must have been rolled back too
	_, insertErr := db.Exec(`INSERT INTO tx_test (id) VALUES (1)`)
	assert.Error(t, insertErr, "tx_test must not exist — transaction was rolled back")

	// drop the trigger so the second run can succeed
	_, err = db.Exec(`DROP TRIGGER block_migrations`)
	require.NoError(t, err)

	// re-run must apply the migration cleanly
	require.NoError(t, m.Run(fsys))

	_, insertErr = db.Exec(`INSERT INTO tx_test (id) VALUES (1)`)
	assert.NoError(t, insertErr, "tx_test must exist after successful re-run")
}

func TestRun_SkipsAlreadyApplied(t *testing.T) {
	db := newDB(t)
	m := migrate.New(db)

	fsys1 := fstest.MapFS{
		"001_init.sql": {Data: []byte(`CREATE TABLE things (id INTEGER PRIMARY KEY);`)},
	}
	require.NoError(t, m.Run(fsys1))

	// add a second migration and run again — only the new one should apply
	fsys2 := fstest.MapFS{
		"001_init.sql": {Data: []byte(`CREATE TABLE things (id INTEGER PRIMARY KEY);`)},
		"002_more.sql": {Data: []byte(`ALTER TABLE things ADD COLUMN val TEXT;`)},
	}
	require.NoError(t, m.Run(fsys2))

	_, err := db.Exec(`INSERT INTO things (val) VALUES ('x')`)
	assert.NoError(t, err)
}
