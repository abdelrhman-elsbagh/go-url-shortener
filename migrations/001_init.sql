CREATE TABLE IF NOT EXISTS urls (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    short_code   TEXT    NOT NULL UNIQUE,
    original_url TEXT    NOT NULL,
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at   DATETIME,
    click_count  INTEGER NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_short_code   ON urls(short_code);
CREATE INDEX        IF NOT EXISTS idx_original_url ON urls(original_url);
CREATE INDEX        IF NOT EXISTS idx_expires_at   ON urls(expires_at);
