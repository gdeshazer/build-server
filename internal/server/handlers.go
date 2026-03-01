package server

import (
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	buildsvc "github.com/grantdeshazer/build-server/internal/build"
	dbpkg "github.com/grantdeshazer/build-server/internal/db"
	"github.com/grantdeshazer/build-server/internal/models"
)

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != s.basePath+"/" {
		http.NotFound(w, r)
		return
	}
	repos, err := dbpkg.ListRepos(s.db)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}

	for _, repo := range repos {
		repo.MakeTarget = s.repoMakeTarget(repo.Name)
		latest, _ := dbpkg.GetLatestBuildRun(s.db, repo.ID)
		repo.LatestBuild = latest
	}

	renderPage(w, "index.html", map[string]any{"Repos": repos, "BasePath": s.basePath})
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	repo, err := dbpkg.GetRepo(s.db, name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	remote := s.repoRemote(name)
	if err := s.gitOps.Fetch(repo.LocalPath, remote); err != nil {
		http.Error(w, fmt.Sprintf("fetch failed: %v", err), http.StatusInternalServerError)
		return
	}

	local, err := s.gitOps.LocalCommitHash(repo.LocalPath, repo.ActiveBranch)
	if err != nil {
		http.Error(w, fmt.Sprintf("local hash: %v", err), http.StatusInternalServerError)
		return
	}
	remote2, err := s.gitOps.RemoteCommitHash(repo.LocalPath, remote, repo.ActiveBranch)
	if err != nil {
		http.Error(w, fmt.Sprintf("remote hash: %v", err), http.StatusInternalServerError)
		return
	}

	if err := dbpkg.UpdateRepoCommits(s.db, name, local, remote2); err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}

	repo, _ = dbpkg.GetRepo(s.db, name)
	repo.MakeTarget = s.repoMakeTarget(name)
	latest, _ := dbpkg.GetLatestBuildRun(s.db, repo.ID)
	repo.LatestBuild = latest

	branches, _ := s.gitOps.ListLocalBranches(repo.LocalPath)
	renderPartial(w, "repo_row.html", map[string]any{
		"Repo":     repo,
		"Branches": branches,
		"BasePath": s.basePath,
	})
}

func (s *Server) handleRefreshAll(w http.ResponseWriter, r *http.Request) {
	repos, err := dbpkg.ListRepos(s.db)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}

	sem := make(chan struct{}, s.cfg.Server.RefreshConcurrency)
	var wg sync.WaitGroup
	for _, repo := range repos {
		wg.Add(1)
		go func(repo *models.Repository) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			remote := s.repoRemote(repo.Name)
			if err := s.gitOps.Fetch(repo.LocalPath, remote); err != nil {
				return
			}
			local, err := s.gitOps.LocalCommitHash(repo.LocalPath, repo.ActiveBranch)
			if err != nil {
				return
			}
			remoteHash, err := s.gitOps.RemoteCommitHash(repo.LocalPath, remote, repo.ActiveBranch)
			if err != nil {
				return
			}
			dbpkg.UpdateRepoCommits(s.db, repo.Name, local, remoteHash)
		}(repo)
	}
	wg.Wait()

	repos, _ = dbpkg.ListRepos(s.db)
	for _, repo := range repos {
		repo.MakeTarget = s.repoMakeTarget(repo.Name)
		latest, _ := dbpkg.GetLatestBuildRun(s.db, repo.ID)
		repo.LatestBuild = latest
	}

	renderPartial(w, "repo_list.html", map[string]any{"Repos": repos, "BasePath": s.basePath})
}

func (s *Server) handlePull(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	repo, err := dbpkg.GetRepo(s.db, name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	remote := s.repoRemote(name)
	if err := s.gitOps.Pull(repo.LocalPath, remote, repo.ActiveBranch); err != nil {
		http.Error(w, fmt.Sprintf("pull failed: %v", err), http.StatusInternalServerError)
		return
	}

	local, err := s.gitOps.LocalCommitHash(repo.LocalPath, repo.ActiveBranch)
	if err != nil {
		http.Error(w, fmt.Sprintf("local hash: %v", err), http.StatusInternalServerError)
		return
	}
	remoteHash, err := s.gitOps.RemoteCommitHash(repo.LocalPath, remote, repo.ActiveBranch)
	if err != nil {
		http.Error(w, fmt.Sprintf("remote hash: %v", err), http.StatusInternalServerError)
		return
	}

	if err := dbpkg.UpdateRepoCommits(s.db, name, local, remoteHash); err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}

	repo, _ = dbpkg.GetRepo(s.db, name)
	repo.MakeTarget = s.repoMakeTarget(name)
	latest, _ := dbpkg.GetLatestBuildRun(s.db, repo.ID)
	repo.LatestBuild = latest

	branches, _ := s.gitOps.ListLocalBranches(repo.LocalPath)
	renderPartial(w, "repo_row.html", map[string]any{
		"Repo":     repo,
		"Branches": branches,
		"BasePath": s.basePath,
	})
}

func (s *Server) handleBuild(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	repo, err := dbpkg.GetRepo(s.db, name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	active, err := dbpkg.HasActiveBuild(s.db, repo.ID)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	if active {
		http.Error(w, "build already in progress", http.StatusConflict)
		return
	}

	makeTarget := s.repoMakeTarget(name)
	buildID, err := dbpkg.InsertBuildRun(s.db, repo.ID, repo.ActiveBranch, repo.LocalCommit, makeTarget)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}

	timeout := time.Duration(s.repoBuildTimeout(name)) * time.Second
	go buildsvc.Run(s.db, buildID, repo.LocalPath, makeTarget, timeout)

	build, _ := dbpkg.GetBuildRun(s.db, buildID)
	renderPartial(w, "build_log.html", map[string]any{
		"Build":    build,
		"RepoName": name,
		"BasePath": s.basePath,
	})
}

func (s *Server) handleBuildStatus(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	build, err := dbpkg.GetBuildRun(s.db, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	renderPartial(w, "build_log.html", map[string]any{
		"Build":    build,
		"RepoName": r.PathValue("name"),
		"BasePath": s.basePath,
	})
}

func (s *Server) handleBranch(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	repo, err := dbpkg.GetRepo(s.db, name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	branch := r.FormValue("branch")
	if branch == "" {
		http.Error(w, "branch required", http.StatusBadRequest)
		return
	}

	if err := dbpkg.UpdateRepoBranch(s.db, name, branch); err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}

	repo, _ = dbpkg.GetRepo(s.db, name)
	repo.MakeTarget = s.repoMakeTarget(name)
	latest, _ := dbpkg.GetLatestBuildRun(s.db, repo.ID)
	repo.LatestBuild = latest

	branches, _ := s.gitOps.ListLocalBranches(repo.LocalPath)
	renderPartial(w, "repo_row.html", map[string]any{
		"Repo":     repo,
		"Branches": branches,
		"BasePath": s.basePath,
	})
}
