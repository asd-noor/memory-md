// Package db handles opening the SQLite cache and applying the schema.
package db

import (
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
)

// Open opens (or creates) the SQLite database at the given path, applies WAL
// mode and foreign-key enforcement via DSN parameters, and runs applySchema.
func Open(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_journal_mode=WAL&_foreign_keys=true", path)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("db.Open: %w", err)
	}
	if err := applySchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("db.Open applySchema: %w", err)
	}
	return db, nil
}

const schema = `
CREATE TABLE IF NOT EXISTS files (
    file_path   TEXT PRIMARY KEY,
    file_mtime  INTEGER NOT NULL,
    title       TEXT,
    description TEXT
);

CREATE TABLE IF NOT EXISTS sections (
    id                 TEXT PRIMARY KEY,
    path               TEXT NOT NULL,
    file_path          TEXT NOT NULL REFERENCES files(file_path) ON DELETE CASCADE,
    heading_start_byte INTEGER NOT NULL,
    start_byte         INTEGER NOT NULL,
    end_byte           INTEGER NOT NULL,
    level              INTEGER NOT NULL,
    heading            TEXT NOT NULL,
    content            TEXT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS sections_path ON sections(path);
CREATE INDEX        IF NOT EXISTS sections_file ON sections(file_path);

CREATE VIRTUAL TABLE IF NOT EXISTS sections_fts USING fts5(
    path,
    heading,
    content,
    content='sections',
    content_rowid='rowid'
);

CREATE VIRTUAL TABLE IF NOT EXISTS sections_vec USING vec0(
    embedding   float[384],
    +section_id TEXT
);

CREATE TRIGGER IF NOT EXISTS sections_fts_insert AFTER INSERT ON sections BEGIN
    INSERT INTO sections_fts(rowid, path, heading, content)
    VALUES (new.rowid, new.path, new.heading, new.content);
END;

CREATE TRIGGER IF NOT EXISTS sections_fts_delete AFTER DELETE ON sections BEGIN
    INSERT INTO sections_fts(sections_fts, rowid, path, heading, content)
    VALUES ('delete', old.rowid, old.path, old.heading, old.content);
END;

CREATE TRIGGER IF NOT EXISTS sections_fts_update AFTER UPDATE ON sections BEGIN
    INSERT INTO sections_fts(sections_fts, rowid, path, heading, content)
    VALUES ('delete', old.rowid, old.path, old.heading, old.content);
    INSERT INTO sections_fts(rowid, path, heading, content)
    VALUES (new.rowid, new.path, new.heading, new.content);
END;
`

func applySchema(db *sql.DB) error {
	_, err := db.Exec(schema)
	return err
}
