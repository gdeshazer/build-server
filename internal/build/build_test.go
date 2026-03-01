package build_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/grantdeshazer/build-server/internal/build"
	"github.com/grantdeshazer/build-server/internal/db"
	"github.com/grantdeshazer/build-server/internal/testutil"
)

// makeMakefile writes a Makefile with the given target recipe into dir.
func makeMakefile(t *testing.T, dir, target, recipe string) {
	t.Helper()
	content := ".PHONY: " + target + "\n" + target + ":\n\t" + recipe + "\n"
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestRun_Success(t *testing.T) {
	database := testutil.OpenTestDB(t)
	dir := t.TempDir()
	makeMakefile(t, dir, "deploy", "echo hello")

	database.Exec(`INSERT INTO repositories (name, local_path, active) VALUES ('r', ?, 1)`, dir)
	var repoID int64
	database.QueryRow("SELECT id FROM repositories WHERE name='r'").Scan(&repoID)

	buildID, err := db.InsertBuildRun(database, repoID, "main", "abc", "deploy")
	if err != nil {
		t.Fatal(err)
	}

	build.Run(database, buildID, dir, "deploy")

	b, err := db.GetBuildRun(database, buildID)
	if err != nil {
		t.Fatal(err)
	}
	if b.Status != "success" {
		t.Errorf("status = %q, want success; log: %s", b.Status, b.LogOutput)
	}
	if b.ExitCode == nil || *b.ExitCode != 0 {
		t.Errorf("exit_code = %v, want 0", b.ExitCode)
	}
}

func TestRun_Failure(t *testing.T) {
	database := testutil.OpenTestDB(t)
	dir := t.TempDir()
	makeMakefile(t, dir, "deploy", "exit 1")

	database.Exec(`INSERT INTO repositories (name, local_path, active) VALUES ('r', ?, 1)`, dir)
	var repoID int64
	database.QueryRow("SELECT id FROM repositories WHERE name='r'").Scan(&repoID)

	buildID, _ := db.InsertBuildRun(database, repoID, "main", "abc", "deploy")
	build.Run(database, buildID, dir, "deploy")

	b, _ := db.GetBuildRun(database, buildID)
	if b.Status != "failed" {
		t.Errorf("status = %q, want failed", b.Status)
	}
	if b.ExitCode == nil || *b.ExitCode == 0 {
		t.Errorf("exit_code = %v, want non-zero", b.ExitCode)
	}
}

func TestRun_MissingMakefile(t *testing.T) {
	database := testutil.OpenTestDB(t)
	dir := t.TempDir() // no Makefile present

	database.Exec(`INSERT INTO repositories (name, local_path, active) VALUES ('r', ?, 1)`, dir)
	var repoID int64
	database.QueryRow("SELECT id FROM repositories WHERE name='r'").Scan(&repoID)

	buildID, _ := db.InsertBuildRun(database, repoID, "main", "abc", "deploy")
	build.Run(database, buildID, dir, "deploy")

	b, _ := db.GetBuildRun(database, buildID)
	if b.Status != "failed" {
		t.Errorf("status = %q, want failed", b.Status)
	}
	if b.ExitCode == nil || *b.ExitCode == 0 {
		t.Errorf("exit_code = %v, want non-zero", b.ExitCode)
	}
}

func TestRun_Lifecycle(t *testing.T) {
	database := testutil.OpenTestDB(t)
	dir := t.TempDir()
	makeMakefile(t, dir, "deploy", "echo lifecycle")

	database.Exec(`INSERT INTO repositories (name, local_path, active) VALUES ('r', ?, 1)`, dir)
	var repoID int64
	database.QueryRow("SELECT id FROM repositories WHERE name='r'").Scan(&repoID)

	buildID, _ := db.InsertBuildRun(database, repoID, "main", "abc", "deploy")

	b, _ := db.GetBuildRun(database, buildID)
	if b.Status != "pending" {
		t.Fatalf("before Run: status = %q, want pending", b.Status)
	}

	build.Run(database, buildID, dir, "deploy")

	b, _ = db.GetBuildRun(database, buildID)
	if b.Status != "success" {
		t.Errorf("after Run: status = %q, want success", b.Status)
	}
	if b.StartedAt == nil {
		t.Error("started_at should be set")
	}
	if b.FinishedAt == nil {
		t.Error("finished_at should be set")
	}
}
