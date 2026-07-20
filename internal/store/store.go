// Package store opens the SQLite database used for session, aliases and pending choices.
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Open opens (and migrates) the SQLite database at path. Caller closes.
func Open(path string) (*sql.DB, error) {
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			path = "./data/mercadona-mcp.db"
		} else {
			path = filepath.Join(home, ".mercadona-mcp", "data.db")
		}
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("store: mkdir %s: %w", dir, err)
		}
	}
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func migrate(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS mercadona_session (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			access_token TEXT NOT NULL,
			refresh_token TEXT,
			customer_id TEXT NOT NULL,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS grocery_aliases (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			alias TEXT NOT NULL UNIQUE,
			product_id TEXT NOT NULL,
			product_name TEXT NOT NULL,
			use_count INTEGER NOT NULL DEFAULT 1,
			last_used TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS grocery_pending (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			alias_text TEXT NOT NULL,
			options_json TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			resolved_at TIMESTAMP
		)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return fmt.Errorf("store: migrate: %w", err)
		}
	}
	return nil
}
