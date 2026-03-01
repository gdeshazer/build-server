package server

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/grantdeshazer/build-server/internal/config"
	dbpkg "github.com/grantdeshazer/build-server/internal/db"
	"github.com/grantdeshazer/build-server/internal/testutil"
)

// makeTestFS returns a minimal fs.FS with stub templates and static files.
func makeTestFS() fstest.MapFS {
	return fstest.MapFS{
		"templates/layout.html": {Data: []byte(
			`{{define "layout"}}LAYOUT:{{block "content" .}}{{end}}{{end}}`,
		)},
		"templates/index.html": {Data: []byte(
			`{{template "layout" .}}{{define "content"}}{{range .Repos}}REPO:{{.Name}} {{end}}{{end}}`,
		)},
		"templates/partials/repo_row.html": {Data: []byte(
			`{{define "repo_row.html"}}REPO_ROW:{{.Repo.Name}}{{end}}`,
		)},
		"templates/partials/repo_list.html": {Data: []byte(
			`{{define "repo_list.html"}}{{range .Repos}}REPO:{{.Name}} {{end}}{{end}}`,
		)},
		"templates/partials/build_log.html": {Data: []byte(
			`{{define "build_log.html"}}STATUS:{{.Build.Status}}{{if not .Build.IsTerminal}} hx-trigger{{end}}{{end}}`,
		)},
		"static/style.css": {Data: []byte(`body{}`)},
	}
}

// TestMain initializes templates once for the entire server test package.
func TestMain(m *testing.M) {
	if err := InitTemplates(makeTestFS()); err != nil {
		fmt.Fprintf(os.Stderr, "InitTemplates: %v\n", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// mockGitOps is a test double for GitOps.
type mockGitOps struct {
	fetchErr    error
	pullErr     error
	localHash   string
	remoteHash  string
	branches    []string
	branchesErr error
}

func (m *mockGitOps) Fetch(path, remote string) error { return m.fetchErr }
func (m *mockGitOps) Pull(path, remote, branch string) error  { return m.pullErr }
func (m *mockGitOps) LocalCommitHash(path, branch string) (string, error) {
	return m.localHash, nil
}
func (m *mockGitOps) RemoteCommitHash(path, remote, branch string) (string, error) {
	return m.remoteHash, nil
}
func (m *mockGitOps) ListLocalBranches(path string) ([]string, error) {
	return m.branches, m.branchesErr
}

// testEnv holds a server and its underlying DB for handler tests.
type testEnv struct {
	srv *Server
	db  *sql.DB
}

// newTestEnv creates a Server wired to an in-memory DB pre-populated with repos.
func newTestEnv(t *testing.T, repos []config.RepoConfig, gitOps GitOps) *testEnv {
	t.Helper()
	database := testutil.OpenTestDB(t)
	if len(repos) > 0 {
		if err := dbpkg.SyncRepos(database, repos); err != nil {
			t.Fatalf("SyncRepos: %v", err)
		}
	}
	cfg := &config.Config{
		Server:       config.ServerConfig{Port: 8080, RefreshConcurrency: 4},
		Repositories: repos,
	}
	srv, err := New(cfg, database, makeTestFS(), "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if gitOps != nil {
		srv.WithGitOps(gitOps)
	}
	return &testEnv{srv: srv, db: database}
}

// serve fires a request through the server's mux.
func (e *testEnv) serve(method, path string, body url.Values) *httptest.ResponseRecorder {
	var reqBody *strings.Reader
	if body != nil {
		reqBody = strings.NewReader(body.Encode())
	} else {
		reqBody = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reqBody)
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	w := httptest.NewRecorder()
	e.srv.ServeHTTP(w, req)
	return w
}

// TestHandleIndex tests the GET / handler.
func TestHandleIndex(t *testing.T) {
	t.Run("empty repo list renders full page", func(t *testing.T) {
		env := newTestEnv(t, nil, nil)
		w := env.serve("GET", "/", nil)
		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", w.Code)
		}
		if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Errorf("Content-Type = %q, want text/html", ct)
		}
	})

	t.Run("repos in DB rendered with names", func(t *testing.T) {
		repos := []config.RepoConfig{
			{Name: "myrepo", Path: "/tmp/m", DefaultBranch: "main"},
		}
		env := newTestEnv(t, repos, nil)
		w := env.serve("GET", "/", nil)
		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", w.Code)
		}
		if !strings.Contains(w.Body.String(), "myrepo") {
			t.Errorf("body should contain 'myrepo', got:\n%s", w.Body.String())
		}
	})

	t.Run("DB error returns 500", func(t *testing.T) {
		env := newTestEnv(t, nil, nil)
		env.db.Exec("DROP TABLE repositories")
		w := env.serve("GET", "/", nil)
		if w.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500", w.Code)
		}
	})
}

// TestHandleRefresh tests POST /repos/{name}/refresh.
func TestHandleRefresh(t *testing.T) {
	repos := []config.RepoConfig{
		{Name: "myrepo", Path: "/tmp/m", DefaultBranch: "main", Remote: "origin"},
	}

	t.Run("unknown repo returns 404", func(t *testing.T) {
		env := newTestEnv(t, repos, &mockGitOps{})
		w := env.serve("POST", "/repos/unknown/refresh", nil)
		if w.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", w.Code)
		}
	})

	t.Run("fetch error returns 500", func(t *testing.T) {
		env := newTestEnv(t, repos, &mockGitOps{fetchErr: fmt.Errorf("network down")})
		w := env.serve("POST", "/repos/myrepo/refresh", nil)
		if w.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500", w.Code)
		}
	})

	t.Run("success updates DB and returns repo_row", func(t *testing.T) {
		mock := &mockGitOps{localHash: "aaa111bbb222ccc333ddd444eee555fff666aaa1", remoteHash: "bbb222ccc333ddd444eee555fff666aaa111bbb2"}
		env := newTestEnv(t, repos, mock)
		w := env.serve("POST", "/repos/myrepo/refresh", nil)
		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "myrepo") {
			t.Errorf("body should contain 'myrepo', got:\n%s", w.Body.String())
		}
		// Verify DB was updated
		r, err := dbpkg.GetRepo(env.db, "myrepo")
		if err != nil {
			t.Fatal(err)
		}
		if r.LocalCommit != mock.localHash {
			t.Errorf("local_commit = %q, want %q", r.LocalCommit, mock.localHash)
		}
	})
}

// TestHandleRefreshAll tests POST /repos/refresh-all.
func TestHandleRefreshAll(t *testing.T) {
	repos := []config.RepoConfig{
		{Name: "repo1", Path: "/tmp/r1", DefaultBranch: "main", Remote: "origin"},
		{Name: "repo2", Path: "/tmp/r2", DefaultBranch: "main", Remote: "origin"},
		{Name: "repo3", Path: "/tmp/r3", DefaultBranch: "main", Remote: "origin"},
	}
	hash := "aaa111bbb222ccc333ddd444eee555fff666aaa1"

	t.Run("all repos fetched and DB updated", func(t *testing.T) {
		mock := &mockGitOps{localHash: hash, remoteHash: hash}
		env := newTestEnv(t, repos, mock)
		w := env.serve("POST", "/repos/refresh-all", nil)
		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", w.Code)
		}
		for _, rc := range repos {
			r, err := dbpkg.GetRepo(env.db, rc.Name)
			if err != nil {
				t.Errorf("GetRepo(%q): %v", rc.Name, err)
				continue
			}
			if r.LocalCommit != hash {
				t.Errorf("repo %q local_commit = %q, want %q", rc.Name, r.LocalCommit, hash)
			}
		}
	})

	t.Run("individual git failures don't prevent others from updating", func(t *testing.T) {
		// fetchErr causes all fetches to fail; others should still attempt (though all fail here)
		// Use a mock where fetch errors — DB should remain at empty commits but no panic
		mock := &mockGitOps{fetchErr: fmt.Errorf("simulated failure")}
		env := newTestEnv(t, repos, mock)
		w := env.serve("POST", "/repos/refresh-all", nil)
		// handler returns 200 even when individual repos fail
		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", w.Code)
		}
	})
}

// TestHandlePull tests POST /repos/{name}/pull.
func TestHandlePull(t *testing.T) {
	repos := []config.RepoConfig{
		{Name: "myrepo", Path: "/tmp/m", DefaultBranch: "main", Remote: "origin"},
	}

	t.Run("unknown repo returns 404", func(t *testing.T) {
		env := newTestEnv(t, repos, &mockGitOps{})
		w := env.serve("POST", "/repos/unknown/pull", nil)
		if w.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", w.Code)
		}
	})

	t.Run("pull error returns 500", func(t *testing.T) {
		env := newTestEnv(t, repos, &mockGitOps{pullErr: fmt.Errorf("merge conflict")})
		w := env.serve("POST", "/repos/myrepo/pull", nil)
		if w.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500", w.Code)
		}
		if !strings.Contains(w.Body.String(), "pull failed") {
			t.Errorf("body should mention pull failed, got: %s", w.Body.String())
		}
	})

	t.Run("success updates DB and returns repo_row", func(t *testing.T) {
		localHash := "aaa111bbb222ccc333ddd444eee555fff666aaa1"
		remoteHash := "aaa111bbb222ccc333ddd444eee555fff666aaa1"
		mock := &mockGitOps{localHash: localHash, remoteHash: remoteHash}
		env := newTestEnv(t, repos, mock)
		w := env.serve("POST", "/repos/myrepo/pull", nil)
		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "myrepo") {
			t.Errorf("body should contain 'myrepo', got:\n%s", w.Body.String())
		}
		r, err := dbpkg.GetRepo(env.db, "myrepo")
		if err != nil {
			t.Fatal(err)
		}
		if r.LocalCommit != localHash {
			t.Errorf("local_commit = %q, want %q", r.LocalCommit, localHash)
		}
		if r.RemoteCommit != remoteHash {
			t.Errorf("remote_commit = %q, want %q", r.RemoteCommit, remoteHash)
		}
	})
}

// TestHandleBuild tests POST /repos/{name}/build.
func TestHandleBuild(t *testing.T) {
	repos := []config.RepoConfig{
		{Name: "myrepo", Path: t.TempDir(), DefaultBranch: "main", MakeTarget: "deploy"},
	}

	t.Run("unknown repo returns 404", func(t *testing.T) {
		env := newTestEnv(t, repos, nil)
		w := env.serve("POST", "/repos/unknown/build", nil)
		if w.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", w.Code)
		}
	})

	t.Run("active build returns 409", func(t *testing.T) {
		env := newTestEnv(t, repos, nil)
		// Insert an active build manually
		var repoID int64
		env.db.QueryRow("SELECT id FROM repositories WHERE name='myrepo'").Scan(&repoID)
		env.db.Exec(`INSERT INTO build_runs (repo_id, branch, make_target, status) VALUES (?, 'main', 'deploy', 'running')`, repoID)

		w := env.serve("POST", "/repos/myrepo/build", nil)
		if w.Code != http.StatusConflict {
			t.Errorf("status = %d, want 409", w.Code)
		}
	})

	t.Run("success: build_run inserted as pending", func(t *testing.T) {
		env := newTestEnv(t, repos, nil)
		w := env.serve("POST", "/repos/myrepo/build", nil)
		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "STATUS:") {
			t.Errorf("body should contain build log, got:\n%s", w.Body.String())
		}
	})
}

// TestHandleBuildStatus tests GET /repos/{name}/build/{id}.
func TestHandleBuildStatus(t *testing.T) {
	repos := []config.RepoConfig{
		{Name: "myrepo", Path: "/tmp/m", DefaultBranch: "main"},
	}

	t.Run("invalid id returns 400", func(t *testing.T) {
		env := newTestEnv(t, repos, nil)
		w := env.serve("GET", "/repos/myrepo/build/notanumber", nil)
		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", w.Code)
		}
	})

	t.Run("unknown id returns 404", func(t *testing.T) {
		env := newTestEnv(t, repos, nil)
		w := env.serve("GET", "/repos/myrepo/build/9999", nil)
		if w.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", w.Code)
		}
	})

	t.Run("pending build returns hx-trigger", func(t *testing.T) {
		env := newTestEnv(t, repos, nil)
		var repoID int64
		env.db.QueryRow("SELECT id FROM repositories WHERE name='myrepo'").Scan(&repoID)
		buildID, _ := dbpkg.InsertBuildRun(env.db, repoID, "main", "abc", "deploy")

		w := env.serve("GET", fmt.Sprintf("/repos/myrepo/build/%d", buildID), nil)
		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", w.Code)
		}
		if !strings.Contains(w.Body.String(), "hx-trigger") {
			t.Errorf("pending build should have hx-trigger, got:\n%s", w.Body.String())
		}
	})

	t.Run("terminal build returns no hx-trigger", func(t *testing.T) {
		env := newTestEnv(t, repos, nil)
		var repoID int64
		env.db.QueryRow("SELECT id FROM repositories WHERE name='myrepo'").Scan(&repoID)
		buildID, _ := dbpkg.InsertBuildRun(env.db, repoID, "main", "abc", "deploy")
		dbpkg.UpdateBuildRunStart(env.db, buildID)
		dbpkg.UpdateBuildRunFinish(env.db, buildID, "success", 0, "done")

		w := env.serve("GET", fmt.Sprintf("/repos/myrepo/build/%d", buildID), nil)
		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", w.Code)
		}
		if strings.Contains(w.Body.String(), "hx-trigger") {
			t.Errorf("terminal build should NOT have hx-trigger, got:\n%s", w.Body.String())
		}
	})
}

// TestHandleBranch tests POST /repos/{name}/branch.
func TestHandleBranch(t *testing.T) {
	repos := []config.RepoConfig{
		{Name: "myrepo", Path: "/tmp/m", DefaultBranch: "main"},
	}

	t.Run("unknown repo returns 404", func(t *testing.T) {
		env := newTestEnv(t, repos, &mockGitOps{})
		form := url.Values{"branch": {"feature"}}
		w := env.serve("POST", "/repos/unknown/branch", form)
		if w.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", w.Code)
		}
	})

	t.Run("missing branch form value returns 400", func(t *testing.T) {
		env := newTestEnv(t, repos, &mockGitOps{})
		w := env.serve("POST", "/repos/myrepo/branch", url.Values{})
		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", w.Code)
		}
	})

	t.Run("success: DB updated and returns repo_row", func(t *testing.T) {
		mock := &mockGitOps{branches: []string{"main", "develop"}}
		env := newTestEnv(t, repos, mock)
		form := url.Values{"branch": {"develop"}}
		w := env.serve("POST", "/repos/myrepo/branch", form)
		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "myrepo") {
			t.Errorf("body should contain 'myrepo', got:\n%s", w.Body.String())
		}
		r, _ := dbpkg.GetRepo(env.db, "myrepo")
		if r.ActiveBranch != "develop" {
			t.Errorf("active_branch = %q, want develop", r.ActiveBranch)
		}
	})
}
