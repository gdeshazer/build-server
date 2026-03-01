package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/grantdeshazer/build-server/internal/config"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(f, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr bool
		check   func(*testing.T, *config.Config)
	}{
		{
			name: "valid full config",
			yaml: "server:\n  port: 9090\n  db_path: /tmp/test.db\n  refresh_concurrency: 2\nrepositories:\n  - name: myrepo\n    path: /tmp/repo\n    remote: upstream\n    default_branch: develop\n    make_target: build\n",
			check: func(t *testing.T, c *config.Config) {
				if c.Server.Port != 9090 {
					t.Errorf("port = %d, want 9090", c.Server.Port)
				}
				if c.Server.DBPath != "/tmp/test.db" {
					t.Errorf("db_path = %q, want /tmp/test.db", c.Server.DBPath)
				}
				if c.Server.RefreshConcurrency != 2 {
					t.Errorf("concurrency = %d, want 2", c.Server.RefreshConcurrency)
				}
				if len(c.Repositories) != 1 {
					t.Fatalf("len(repos) = %d, want 1", len(c.Repositories))
				}
				r := c.Repositories[0]
				if r.Name != "myrepo" {
					t.Errorf("name = %q, want myrepo", r.Name)
				}
				if r.Remote != "upstream" {
					t.Errorf("remote = %q, want upstream", r.Remote)
				}
				if r.DefaultBranch != "develop" {
					t.Errorf("default_branch = %q, want develop", r.DefaultBranch)
				}
				if r.MakeTarget != "build" {
					t.Errorf("make_target = %q, want build", r.MakeTarget)
				}
			},
		},
		{
			name: "defaults applied",
			yaml: "repositories:\n  - name: repo1\n    path: /tmp/r1\n",
			check: func(t *testing.T, c *config.Config) {
				if c.Server.Port != 8080 {
					t.Errorf("port = %d, want 8080", c.Server.Port)
				}
				if c.Server.DBPath != "./build-server.db" {
					t.Errorf("db_path = %q, want ./build-server.db", c.Server.DBPath)
				}
				if c.Server.RefreshConcurrency != 4 {
					t.Errorf("concurrency = %d, want 4", c.Server.RefreshConcurrency)
				}
				r := c.Repositories[0]
				if r.Remote != "origin" {
					t.Errorf("remote = %q, want origin", r.Remote)
				}
				if r.DefaultBranch != "main" {
					t.Errorf("default_branch = %q, want main", r.DefaultBranch)
				}
				if r.MakeTarget != "deploy" {
					t.Errorf("make_target = %q, want deploy", r.MakeTarget)
				}
				if r.BuildTimeout != 60 {
					t.Errorf("build_timeout = %d, want 60", r.BuildTimeout)
				}
			},
		},
		{
			name: "explicit build_timeout",
			yaml: "repositories:\n  - name: repo1\n    path: /tmp/r1\n    build_timeout: 120\n",
			check: func(t *testing.T, c *config.Config) {
				r := c.Repositories[0]
				if r.BuildTimeout != 120 {
					t.Errorf("build_timeout = %d, want 120", r.BuildTimeout)
				}
			},
		},
		{
			name:    "error: missing repo name",
			yaml:    "repositories:\n  - path: /tmp/r1\n",
			wantErr: true,
		},
		{
			name:    "error: missing repo path",
			yaml:    "repositories:\n  - name: myrepo\n",
			wantErr: true,
		},
		{
			name:    "error: duplicate repo name",
			yaml:    "repositories:\n  - name: dup\n    path: /tmp/a\n  - name: dup\n    path: /tmp/b\n",
			wantErr: true,
		},
		{
			name:    "error: file not found",
			wantErr: true,
		},
		{
			name:    "error: malformed YAML",
			yaml:    "server: [invalid\n",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var path string
			if tc.name == "error: file not found" {
				path = "/nonexistent/path/config.yaml"
			} else {
				path = writeConfig(t, tc.yaml)
			}

			cfg, err := config.Load(path)
			if tc.wantErr {
				if err == nil {
					t.Error("want error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if tc.check != nil {
				tc.check(t, cfg)
			}
		})
	}
}
