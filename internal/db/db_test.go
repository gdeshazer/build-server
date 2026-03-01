package db_test

import (
	"path/filepath"
	"testing"

	"github.com/grantdeshazer/build-server/internal/config"
	"github.com/grantdeshazer/build-server/internal/db"
	"github.com/grantdeshazer/build-server/internal/testutil"
)

func TestOpen(t *testing.T) {
	database := testutil.OpenTestDB(t)

	tables := []string{"repositories", "build_runs", "schema_version"}
	for _, table := range tables {
		t.Run(table+" exists", func(t *testing.T) {
			var name string
			err := database.QueryRow(
				"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table,
			).Scan(&name)
			if err != nil {
				t.Errorf("table %q not found: %v", table, err)
			}
		})
	}
}

func TestMigrateIdempotent(t *testing.T) {
	// Open a file-based DB, close it, open again — migrate runs twice.
	path := filepath.Join(t.TempDir(), "test.db")

	db1, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	db1.Close()

	db2, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()

	var count int
	if err := db2.QueryRow("SELECT COUNT(*) FROM schema_version").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("schema_version rows = %d, want 1", count)
	}
}

func TestSyncRepos(t *testing.T) {
	t.Run("inserts new repos from config", func(t *testing.T) {
		database := testutil.OpenTestDB(t)
		repos := []config.RepoConfig{
			{Name: "alpha", Path: "/tmp/alpha", DefaultBranch: "main"},
			{Name: "beta", Path: "/tmp/beta", DefaultBranch: "main"},
		}
		if err := db.SyncRepos(database, repos); err != nil {
			t.Fatal(err)
		}

		var count int
		database.QueryRow("SELECT COUNT(*) FROM repositories WHERE active=1").Scan(&count)
		if count != 2 {
			t.Errorf("active repos = %d, want 2", count)
		}
	})

	t.Run("upserts existing repo path", func(t *testing.T) {
		database := testutil.OpenTestDB(t)
		repos := []config.RepoConfig{{Name: "myrepo", Path: "/tmp/old", DefaultBranch: "main"}}
		if err := db.SyncRepos(database, repos); err != nil {
			t.Fatal(err)
		}

		repos[0].Path = "/tmp/new"
		if err := db.SyncRepos(database, repos); err != nil {
			t.Fatal(err)
		}

		var path string
		database.QueryRow("SELECT local_path FROM repositories WHERE name='myrepo'").Scan(&path)
		if path != "/tmp/new" {
			t.Errorf("local_path = %q, want /tmp/new", path)
		}
	})

	t.Run("soft-deletes repo not in config", func(t *testing.T) {
		database := testutil.OpenTestDB(t)
		initial := []config.RepoConfig{
			{Name: "keep", Path: "/tmp/keep", DefaultBranch: "main"},
			{Name: "gone", Path: "/tmp/gone", DefaultBranch: "main"},
		}
		if err := db.SyncRepos(database, initial); err != nil {
			t.Fatal(err)
		}

		updated := []config.RepoConfig{{Name: "keep", Path: "/tmp/keep", DefaultBranch: "main"}}
		if err := db.SyncRepos(database, updated); err != nil {
			t.Fatal(err)
		}

		var active int
		database.QueryRow("SELECT active FROM repositories WHERE name='gone'").Scan(&active)
		if active != 0 {
			t.Errorf("gone repo active = %d, want 0", active)
		}
		// Row still exists
		var count int
		database.QueryRow("SELECT COUNT(*) FROM repositories WHERE name='gone'").Scan(&count)
		if count != 1 {
			t.Errorf("gone repo row count = %d, want 1 (soft delete preserves row)", count)
		}
	})

	t.Run("preserves active_branch of active repo on re-sync", func(t *testing.T) {
		database := testutil.OpenTestDB(t)
		repos := []config.RepoConfig{{Name: "mrepo", Path: "/tmp/m", DefaultBranch: "main"}}
		if err := db.SyncRepos(database, repos); err != nil {
			t.Fatal(err)
		}
		// Simulate user changing the branch
		database.Exec("UPDATE repositories SET active_branch='feature' WHERE name='mrepo'")

		// Re-sync — branch should be preserved since repo is active
		if err := db.SyncRepos(database, repos); err != nil {
			t.Fatal(err)
		}

		var branch string
		database.QueryRow("SELECT active_branch FROM repositories WHERE name='mrepo'").Scan(&branch)
		if branch != "feature" {
			t.Errorf("active_branch = %q, want feature", branch)
		}
	})

	t.Run("re-added soft-deleted repo becomes active", func(t *testing.T) {
		database := testutil.OpenTestDB(t)
		repos := []config.RepoConfig{{Name: "toggled", Path: "/tmp/t", DefaultBranch: "main"}}

		// Add, then remove
		if err := db.SyncRepos(database, repos); err != nil {
			t.Fatal(err)
		}
		if err := db.SyncRepos(database, nil); err != nil {
			t.Fatal(err)
		}
		var active int
		database.QueryRow("SELECT active FROM repositories WHERE name='toggled'").Scan(&active)
		if active != 0 {
			t.Errorf("after removal: active = %d, want 0", active)
		}

		// Re-add
		if err := db.SyncRepos(database, repos); err != nil {
			t.Fatal(err)
		}
		database.QueryRow("SELECT active FROM repositories WHERE name='toggled'").Scan(&active)
		if active != 1 {
			t.Errorf("after re-add: active = %d, want 1", active)
		}
	})
}
