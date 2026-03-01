package db_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/grantdeshazer/build-server/internal/db"
	"github.com/grantdeshazer/build-server/internal/testutil"
)

// addRepo inserts a minimal active repository and returns its ID.
func addRepo(t *testing.T, database *sql.DB, name, path string) int64 {
	t.Helper()
	res, err := database.Exec(
		`INSERT INTO repositories (name, local_path, active) VALUES (?, ?, 1)`, name, path,
	)
	if err != nil {
		t.Fatalf("addRepo: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

func TestListRepos(t *testing.T) {
	database := testutil.OpenTestDB(t)
	addRepo(t, database, "zebra", "/z")
	addRepo(t, database, "apple", "/a")
	// inactive repo — should not appear
	database.Exec(`INSERT INTO repositories (name, local_path, active) VALUES ('hidden', '/h', 0)`)

	repos, err := db.ListRepos(database)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 2 {
		t.Fatalf("len(repos) = %d, want 2", len(repos))
	}
	if repos[0].Name != "apple" || repos[1].Name != "zebra" {
		t.Errorf("order: got [%s, %s], want [apple, zebra]", repos[0].Name, repos[1].Name)
	}
}

func TestGetRepo(t *testing.T) {
	database := testutil.OpenTestDB(t)
	addRepo(t, database, "existing", "/e")
	database.Exec(`INSERT INTO repositories (name, local_path, active) VALUES ('inactive', '/i', 0)`)

	t.Run("found", func(t *testing.T) {
		r, err := db.GetRepo(database, "existing")
		if err != nil {
			t.Fatalf("GetRepo: %v", err)
		}
		if r.Name != "existing" {
			t.Errorf("name = %q, want existing", r.Name)
		}
	})

	t.Run("not found", func(t *testing.T) {
		_, err := db.GetRepo(database, "missing")
		if err == nil {
			t.Error("want error for missing repo, got nil")
		}
	})

	t.Run("inactive treated as not found", func(t *testing.T) {
		_, err := db.GetRepo(database, "inactive")
		if err == nil {
			t.Error("want error for inactive repo, got nil")
		}
	})
}

func TestUpdateRepoCommits(t *testing.T) {
	database := testutil.OpenTestDB(t)
	addRepo(t, database, "r", "/r")

	if err := db.UpdateRepoCommits(database, "r", "aaa111", "bbb222"); err != nil {
		t.Fatalf("UpdateRepoCommits: %v", err)
	}

	r, _ := db.GetRepo(database, "r")
	if r.LocalCommit != "aaa111" {
		t.Errorf("local_commit = %q, want aaa111", r.LocalCommit)
	}
	if r.RemoteCommit != "bbb222" {
		t.Errorf("remote_commit = %q, want bbb222", r.RemoteCommit)
	}
	if r.LastRefreshed == nil {
		t.Error("last_refreshed should be set after UpdateRepoCommits")
	}
}

func TestUpdateRepoBranch(t *testing.T) {
	database := testutil.OpenTestDB(t)
	database.Exec(`INSERT INTO repositories (name, local_path, active, local_commit, remote_commit) VALUES ('r', '/r', 1, 'abc', 'def')`)

	if err := db.UpdateRepoBranch(database, "r", "develop"); err != nil {
		t.Fatalf("UpdateRepoBranch: %v", err)
	}

	r, _ := db.GetRepo(database, "r")
	if r.ActiveBranch != "develop" {
		t.Errorf("active_branch = %q, want develop", r.ActiveBranch)
	}
	if r.LocalCommit != "" {
		t.Errorf("local_commit = %q, want empty after branch change", r.LocalCommit)
	}
	if r.RemoteCommit != "" {
		t.Errorf("remote_commit = %q, want empty after branch change", r.RemoteCommit)
	}
}

func TestHasActiveBuild(t *testing.T) {
	database := testutil.OpenTestDB(t)
	repoID := addRepo(t, database, "r", "/r")

	t.Run("false with no builds", func(t *testing.T) {
		active, err := db.HasActiveBuild(database, repoID)
		if err != nil {
			t.Fatal(err)
		}
		if active {
			t.Error("want false, got true")
		}
	})

	insertBuild := func(status string) {
		database.Exec(
			`INSERT INTO build_runs (repo_id, branch, make_target, status) VALUES (?, 'main', 'deploy', ?)`,
			repoID, status,
		)
	}
	clearBuilds := func() { database.Exec("DELETE FROM build_runs WHERE repo_id=?", repoID) }

	t.Run("true for pending", func(t *testing.T) {
		insertBuild("pending")
		defer clearBuilds()
		active, _ := db.HasActiveBuild(database, repoID)
		if !active {
			t.Error("want true for pending, got false")
		}
	})

	t.Run("true for running", func(t *testing.T) {
		insertBuild("running")
		defer clearBuilds()
		active, _ := db.HasActiveBuild(database, repoID)
		if !active {
			t.Error("want true for running, got false")
		}
	})

	t.Run("false for terminal statuses", func(t *testing.T) {
		for _, status := range []string{"success", "failed", "cancelled"} {
			insertBuild(status)
			active, _ := db.HasActiveBuild(database, repoID)
			if active {
				t.Errorf("want false for %q, got true", status)
			}
			clearBuilds()
		}
	})
}

func TestInsertBuildRun(t *testing.T) {
	database := testutil.OpenTestDB(t)
	repoID := addRepo(t, database, "r", "/r")

	buildID, err := db.InsertBuildRun(database, repoID, "main", "abc123", "deploy")
	if err != nil {
		t.Fatalf("InsertBuildRun: %v", err)
	}
	if buildID == 0 {
		t.Error("buildID should be non-zero")
	}

	b, err := db.GetBuildRun(database, buildID)
	if err != nil {
		t.Fatal(err)
	}
	if b.Status != "pending" {
		t.Errorf("status = %q, want pending", b.Status)
	}
	if b.Branch != "main" {
		t.Errorf("branch = %q, want main", b.Branch)
	}
}

func TestBuildRunLifecycle(t *testing.T) {
	database := testutil.OpenTestDB(t)
	repoID := addRepo(t, database, "r", "/r")

	t.Run("pending to success", func(t *testing.T) {
		buildID, _ := db.InsertBuildRun(database, repoID, "main", "abc", "deploy")

		if err := db.UpdateBuildRunStart(database, buildID); err != nil {
			t.Fatal(err)
		}
		b, _ := db.GetBuildRun(database, buildID)
		if b.Status != "running" {
			t.Errorf("after start: status = %q, want running", b.Status)
		}
		if b.StartedAt == nil {
			t.Error("started_at should be set after UpdateBuildRunStart")
		}

		if err := db.UpdateBuildRunFinish(database, buildID, "success", 0, "build output"); err != nil {
			t.Fatal(err)
		}
		b, _ = db.GetBuildRun(database, buildID)
		if b.Status != "success" {
			t.Errorf("status = %q, want success", b.Status)
		}
		if b.ExitCode == nil || *b.ExitCode != 0 {
			t.Errorf("exit_code = %v, want 0", b.ExitCode)
		}
		if b.LogOutput != "build output" {
			t.Errorf("log_output = %q, want 'build output'", b.LogOutput)
		}
		if b.FinishedAt == nil {
			t.Error("finished_at should be set")
		}
	})

	t.Run("pending to failed", func(t *testing.T) {
		buildID, _ := db.InsertBuildRun(database, repoID, "main", "abc", "deploy")
		db.UpdateBuildRunStart(database, buildID)
		db.UpdateBuildRunFinish(database, buildID, "failed", 1, "error output")

		b, _ := db.GetBuildRun(database, buildID)
		if b.Status != "failed" {
			t.Errorf("status = %q, want failed", b.Status)
		}
		if b.ExitCode == nil || *b.ExitCode != 1 {
			t.Errorf("exit_code = %v, want 1", b.ExitCode)
		}
	})
}

func TestUpdateBuildRunFinish_LogCap(t *testing.T) {
	database := testutil.OpenTestDB(t)
	repoID := addRepo(t, database, "r", "/r")

	buildID, _ := db.InsertBuildRun(database, repoID, "main", "abc", "deploy")
	db.UpdateBuildRunStart(database, buildID)

	bigLog := strings.Repeat("x", 600*1024)
	db.UpdateBuildRunFinish(database, buildID, "success", 0, bigLog)

	b, _ := db.GetBuildRun(database, buildID)
	const maxLog = 512 * 1024
	if len(b.LogOutput) > maxLog {
		t.Errorf("log_output length = %d, exceeds 512KB cap of %d", len(b.LogOutput), maxLog)
	}
}

func TestGetLatestBuildRun(t *testing.T) {
	database := testutil.OpenTestDB(t)
	repoID := addRepo(t, database, "r", "/r")

	t.Run("nil for repo with no builds", func(t *testing.T) {
		b, err := db.GetLatestBuildRun(database, repoID)
		if err != nil {
			t.Fatal(err)
		}
		if b != nil {
			t.Errorf("want nil, got %+v", b)
		}
	})

	t.Run("returns most recent by id", func(t *testing.T) {
		_, _ = db.InsertBuildRun(database, repoID, "main", "aaa", "deploy")
		id2, _ := db.InsertBuildRun(database, repoID, "main", "bbb", "deploy")

		b, err := db.GetLatestBuildRun(database, repoID)
		if err != nil {
			t.Fatal(err)
		}
		if b.ID != id2 {
			t.Errorf("latest ID = %d, want %d", b.ID, id2)
		}
	})
}
