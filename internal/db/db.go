package db

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/grantdeshazer/build-server/internal/config"
	"github.com/grantdeshazer/build-server/internal/logger"
	_ "modernc.org/sqlite"
)

func Open(path string) (*sql.DB, error) {
	logger.Info("DB: Opening database at: %s", path)

	db, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		logger.Error("DB: Failed to open sqlite database at %s: %v", path, err)
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	logger.Debug("DB: Database connection established, max open connections set to 1")

	logger.Info("DB: Running database migrations")
	if err := migrate(db); err != nil {
		logger.Error("DB: Database migration failed: %v", err)
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	logger.Info("DB: Database migrations completed successfully")

	return db, nil
}

func migrate(db *sql.DB) error {
	logger.Debug("DB: Creating schema_version table if not exists")
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_version (
			version INTEGER PRIMARY KEY,
			applied_at DATETIME DEFAULT (datetime('now'))
		)
	`)
	if err != nil {
		logger.Error("DB: Failed to create schema_version table: %v", err)
		return err
	}

	var current int
	row := db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`)
	if err := row.Scan(&current); err != nil {
		logger.Error("DB: Failed to get current schema version: %v", err)
		return err
	}
	logger.Info("DB: Current schema version: %d", current)

	migrations := []func(*sql.Tx) error{
		migration1,
	}

	for i, m := range migrations {
		version := i + 1
		if version <= current {
			logger.Debug("DB: Skipping migration %d (already applied)", version)
			continue
		}
		logger.Info("DB: Applying migration %d", version)
		tx, err := db.Begin()
		if err != nil {
			logger.Error("DB: Failed to begin transaction for migration %d: %v", version, err)
			return err
		}
		if err := m(tx); err != nil {
			logger.Error("DB: Migration %d failed: %v", version, err)
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", version, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_version (version) VALUES (?)`, version); err != nil {
			logger.Error("DB: Failed to record migration %d in schema_version: %v", version, err)
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			logger.Error("DB: Failed to commit migration %d: %v", version, err)
			return err
		}
		logger.Info("DB: Migration %d applied successfully", version)
	}
	return nil
}

func migration1(tx *sql.Tx) error {
	logger.Debug("DB: Migration 1 - Creating repositories and build_runs tables")
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
	if err != nil {
		logger.Error("DB: Migration 1 failed to create tables: %v", err)
	} else {
		logger.Debug("DB: Migration 1 - Tables created successfully")
	}
	return err
}

// SyncRepos upserts active repos from config and soft-deletes removed ones.
// Active repos preserve their active_branch; soft-deleted repos have their
// branch reset to the config default when re-added.
func SyncRepos(db *sql.DB, repos []config.RepoConfig) error {
	logger.Info("DB: Syncing %d repositories from config", len(repos))

	tx, err := db.Begin()
	if err != nil {
		logger.Error("DB: Failed to begin transaction for repo sync: %v", err)
		return err
	}
	defer tx.Rollback()

	for _, r := range repos {
		logger.Debug("DB: Upserting repository '%s' (path: %s, branch: %s)", r.Name, r.Path, r.DefaultBranch)
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
			logger.Error("DB: Failed to upsert repository '%s': %v", r.Name, err)
			return fmt.Errorf("upsert repo %q: %w", r.Name, err)
		}
		logger.Info("DB: Successfully upserted repository '%s'", r.Name)
	}

	// Soft-delete repos no longer in config.
	if len(repos) == 0 {
		logger.Debug("DB: No repos in config, soft-deleting all repositories")
		_, err = tx.Exec(`UPDATE repositories SET active = 0`)
	} else {
		names := make([]any, len(repos))
		placeholders := make([]string, len(repos))
		for i, r := range repos {
			names[i] = r.Name
			placeholders[i] = "?"
		}
		q := `UPDATE repositories SET active = 0 WHERE name NOT IN (` + strings.Join(placeholders, ",") + `)`
		logger.Debug("DB: Soft-deleting repositories not in config (keeping: %v)", names)
		_, err = tx.Exec(q, names...)
	}
	if err != nil {
		logger.Error("DB: Failed to soft-delete removed repositories: %v", err)
		return err
	}

	if err := tx.Commit(); err != nil {
		logger.Error("DB: Failed to commit repository sync transaction: %v", err)
		return err
	}

	logger.Info("DB: Repository sync completed successfully")
	return nil
}
