package git_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grantdeshazer/build-server/internal/git"
)

// makeTestRepo creates a bare remote and a local clone with an initial commit on "main".
// Returns (localPath, remotePath).
func makeTestRepo(t *testing.T) (localPath, remotePath string) {
	t.Helper()
	remotePath = t.TempDir()
	localPath = t.TempDir()

	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
		}
	}

	run(remotePath, "-c", "init.defaultBranch=main", "init", "--bare")
	run(localPath, "clone", remotePath, ".")
	run(localPath, "config", "user.email", "test@example.com")
	run(localPath, "config", "user.name", "Test User")

	if err := os.WriteFile(filepath.Join(localPath, "README.md"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	run(localPath, "add", ".")
	run(localPath, "commit", "-m", "initial")
	run(localPath, "push", "-u", "origin", "main")

	return localPath, remotePath
}

func TestFetch(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		localPath, _ := makeTestRepo(t)
		if err := git.Fetch(localPath, "origin"); err != nil {
			t.Errorf("Fetch: %v", err)
		}
	})

	t.Run("bad path errors", func(t *testing.T) {
		if err := git.Fetch("/nonexistent/path", "origin"); err == nil {
			t.Error("want error for bad path, got nil")
		}
	})
}

func TestLocalCommitHash(t *testing.T) {
	localPath, _ := makeTestRepo(t)

	t.Run("returns SHA for HEAD on main", func(t *testing.T) {
		hash, err := git.LocalCommitHash(localPath, "main")
		if err != nil {
			t.Fatalf("LocalCommitHash: %v", err)
		}
		if len(hash) != 40 {
			t.Errorf("hash length = %d, want 40; hash = %q", len(hash), hash)
		}
	})

	t.Run("errors on unknown branch", func(t *testing.T) {
		_, err := git.LocalCommitHash(localPath, "nonexistent-branch")
		if err == nil {
			t.Error("want error for unknown branch, got nil")
		}
	})
}

func TestRemoteCommitHash(t *testing.T) {
	localPath, _ := makeTestRepo(t)

	t.Run("returns SHA after fetch", func(t *testing.T) {
		if err := git.Fetch(localPath, "origin"); err != nil {
			t.Fatal(err)
		}
		hash, err := git.RemoteCommitHash(localPath, "origin", "main")
		if err != nil {
			t.Fatalf("RemoteCommitHash: %v", err)
		}
		if len(hash) != 40 {
			t.Errorf("hash length = %d, want 40; hash = %q", len(hash), hash)
		}
	})

	t.Run("errors on unknown remote branch", func(t *testing.T) {
		_, err := git.RemoteCommitHash(localPath, "origin", "nonexistent-branch")
		if err == nil {
			t.Error("want error for unknown remote branch, got nil")
		}
	})
}

func TestListLocalBranches(t *testing.T) {
	localPath, _ := makeTestRepo(t)

	t.Run("returns main after initial commit", func(t *testing.T) {
		branches, err := git.ListLocalBranches(localPath)
		if err != nil {
			t.Fatalf("ListLocalBranches: %v", err)
		}
		if len(branches) != 1 || branches[0] != "main" {
			t.Errorf("branches = %v, want [main]", branches)
		}
	})

	t.Run("returns multiple after creating branches", func(t *testing.T) {
		cmd := exec.Command("git", "-C", localPath, "checkout", "-b", "feature/foo")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("create branch: %v\n%s", err, out)
		}

		branches, err := git.ListLocalBranches(localPath)
		if err != nil {
			t.Fatalf("ListLocalBranches: %v", err)
		}
		if len(branches) < 2 {
			t.Errorf("branches = %v, want at least 2", branches)
		}
		found := false
		for _, b := range branches {
			if strings.Contains(b, "feature/foo") || b == "feature/foo" {
				found = true
			}
		}
		if !found {
			t.Errorf("branches = %v, want to contain feature/foo", branches)
		}
	})
}
