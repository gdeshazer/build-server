package db

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/grantdeshazer/build-server/internal/config"
	_ "modernc.org/sqlite"
)

func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)

	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return db, nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_version (
			version INTEGER PRIMARY KEY,
			applied_at DATETIME DEFAULT (datetime('now'))
		)
	`)
	if err != nil {
		return err
	}

	var current int
	row := db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`)
	if err := row.Scan(&current); err != nil {
		return err
	}

	migrations := []func(*sql.Tx) error{
		migration1,
	}

	for i, m := range migrations {
		version := i + 1
		if version <= current {
			continue
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if err := m(tx); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", version, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_version (version) VALUES (?)`, version); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func migration1(tx *sql.Tx) error {
	_, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS repositories (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			name            TEXT NOT NULL UNIQUE,
			local_path      TEXT NOT NULL,
			remote_url      TEXT NOT NULL DEFAULT '',
			active_branch   TEXT NOT NULL DEFAULT 'main',
			local_commit    TEXT,
			remote_commit   TEXT,
			last_refreshed  DATETIME,
			active          INTEGER NOT NULL DEFAULT 1,
			created_at      DATETIME DEFAULT (datetime('now')),
			updated_at      DATETIME DEFAULT (datetime('now'))
		);

		CREATE TABLE IF NOT EXISTS build_runs (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			repo_id         INTEGER NOT NULL REFERENCES repositories(id),
			branch          TEXT NOT NULL,
			commit_hash     TEXT,
			make_target     TEXT NOT NULL DEFAULT 'deploy',
			status          TEXT NOT NULL DEFAULT 'pending',
			exit_code       INTEGER,
			log_output      TEXT,
			started_at      DATETIME,
			finished_at     DATETIME,
			created_at      DATETIME DEFAULT (datetime('now'))
		);
	`)
	return err
}

// SyncRepos upserts active repos from config and soft-deletes removed ones.
// Active repos preserve their active_branch; soft-deleted repos have their
// branch reset to the config default when re-added.
func SyncRepos(db *sql.DB, repos []config.RepoConfig) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, r := range repos {
		_, err := tx.Exec(`
			INSERT INTO repositories (name, local_path, active_branch, active)
			VALUES (?, ?, ?, 1)
			ON CONFLICT(name) DO UPDATE SET
				local_path = excluded.local_path,
				active_branch = CASE WHEN active = 0 THEN excluded.active_branch ELSE active_branch END,
				active = 1,
				updated_at = datetime('now')
		`, r.Name, r.Path, r.DefaultBranch)
		if err != nil {
			return fmt.Errorf("upsert repo %q: %w", r.Name, err)
		}
	}

	// Soft-delete repos no longer in config.
	if len(repos) == 0 {
		_, err = tx.Exec(`UPDATE repositories SET active = 0`)
	} else {
		names := make([]any, len(repos))
		placeholders := make([]string, len(repos))
		for i, r := range repos {
			names[i] = r.Name
			placeholders[i] = "?"
		}
		q := `UPDATE repositories SET active = 0 WHERE name NOT IN (` + strings.Join(placeholders, ",") + `)`
		_, err = tx.Exec(q, names...)
	}
	if err != nil {
		return err
	}

	return tx.Commit()
}
