package server

import (
	"database/sql"
	"fmt"
	"io/fs"
	"net/http"

	"github.com/grantdeshazer/build-server/internal/config"
	"github.com/grantdeshazer/build-server/internal/git"
)

// GitOps abstracts git operations so handlers can be tested without real repos.
type GitOps interface {
	Fetch(path, remote string) error
	LocalCommitHash(path, branch string) (string, error)
	RemoteCommitHash(path, remote, branch string) (string, error)
	ListLocalBranches(path string) ([]string, error)
}

type realGitOps struct{}

func (realGitOps) Fetch(path, remote string) error { return git.Fetch(path, remote) }
func (realGitOps) LocalCommitHash(path, branch string) (string, error) {
	return git.LocalCommitHash(path, branch)
}
func (realGitOps) RemoteCommitHash(path, remote, branch string) (string, error) {
	return git.RemoteCommitHash(path, remote, branch)
}
func (realGitOps) ListLocalBranches(path string) ([]string, error) {
	return git.ListLocalBranches(path)
}

type Server struct {
	cfg      *config.Config
	db       *sql.DB
	mux      *http.ServeMux
	staticFS fs.FS
	gitOps   GitOps
}

func New(cfg *config.Config, db *sql.DB, fsys fs.FS) (*Server, error) {
	staticFS, err := fs.Sub(fsys, "static")
	if err != nil {
		return nil, fmt.Errorf("static fs: %w", err)
	}

	if err := InitTemplates(fsys); err != nil {
		return nil, fmt.Errorf("init templates: %w", err)
	}

	s := &Server{cfg: cfg, db: db, mux: http.NewServeMux(), staticFS: staticFS, gitOps: realGitOps{}}
	s.registerRoutes()
	return s, nil
}

// WithGitOps replaces the git operations implementation. Used in tests.
func (s *Server) WithGitOps(g GitOps) { s.gitOps = g }

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("GET /", s.handleIndex)
	s.mux.HandleFunc("POST /repos/refresh-all", s.handleRefreshAll)
	s.mux.HandleFunc("POST /repos/{name}/refresh", s.handleRefresh)
	s.mux.HandleFunc("POST /repos/{name}/build", s.handleBuild)
	s.mux.HandleFunc("GET /repos/{name}/build/{id}", s.handleBuildStatus)
	s.mux.HandleFunc("POST /repos/{name}/branch", s.handleBranch)
	s.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(s.staticFS))))
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) Addr() string {
	return fmt.Sprintf(":%d", s.cfg.Server.Port)
}

func (s *Server) repoMakeTarget(name string) string {
	for _, r := range s.cfg.Repositories {
		if r.Name == name {
			return r.MakeTarget
		}
	}
	return "deploy"
}

func (s *Server) repoRemote(name string) string {
	for _, r := range s.cfg.Repositories {
		if r.Name == name {
			return r.Remote
		}
	}
	return "origin"
}
