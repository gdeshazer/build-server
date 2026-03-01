package server

import (
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	buildsvc "github.com/grantdeshazer/build-server/internal/build"
	dbpkg "github.com/grantdeshazer/build-server/internal/db"
	"github.com/grantdeshazer/build-server/internal/logger"
	"github.com/grantdeshazer/build-server/internal/models"
)

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	logger.Debug("HTTP: %s %s", r.Method, r.URL.Path)

	if r.URL.Path != s.basePath+"/" {
		http.NotFound(w, r)
		return
	}

	logger.Info("HTTP: Serving index page")
	repos, err := dbpkg.ListRepos(s.db)
	if err != nil {
		logger.Error("HTTP: Failed to list repositories: %v", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}

	for _, repo := range repos {
		repo.MakeTarget = s.repoMakeTarget(repo.Name)
		latest, _ := dbpkg.GetLatestBuildRun(s.db, repo.ID)
		repo.LatestBuild = latest
	}

	logger.Info("HTTP: Rendering index page with %d repositories", len(repos))
	renderPage(w, "index.html", map[string]any{"Repos": repos, "BasePath": s.basePath})
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	logger.Info("HTTP: %s %s - Refreshing repository '%s'", r.Method, r.URL.Path, name)

	repo, err := dbpkg.GetRepo(s.db, name)
	if err != nil {
		logger.Error("HTTP: Failed to get repository '%s': %v", name, err)
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	remote := s.repoRemote(name)
	logger.Info("HTTP: Fetching repository '%s' from remote '%s'", name, remote)
	if err := s.gitOps.Fetch(repo.LocalPath, remote); err != nil {
		logger.Error("HTTP: Fetch failed for repository '%s': %v", name, err)
		http.Error(w, fmt.Sprintf("fetch failed: %v", err), http.StatusInternalServerError)
		return
	}
	logger.Info("HTTP: Fetch completed for repository '%s'", name)

	logger.Debug("HTTP: Getting local commit hash for repository '%s' on branch '%s'", name, repo.ActiveBranch)
	local, err := s.gitOps.LocalCommitHash(repo.LocalPath, repo.ActiveBranch)
	if err != nil {
		logger.Error("HTTP: Failed to get local commit hash for repository '%s': %v", name, err)
		http.Error(w, fmt.Sprintf("local hash: %v", err), http.StatusInternalServerError)
		return
	}
	logger.Debug("HTTP: Local commit hash for '%s': %s", name, local)

	logger.Debug("HTTP: Getting remote commit hash for repository '%s' on remote '%s' branch '%s'", name, remote, repo.ActiveBranch)
	remote2, err := s.gitOps.RemoteCommitHash(repo.LocalPath, remote, repo.ActiveBranch)
	if err != nil {
		logger.Error("HTTP: Failed to get remote commit hash for repository '%s': %v", name, err)
		http.Error(w, fmt.Sprintf("remote hash: %v", err), http.StatusInternalServerError)
		return
	}
	logger.Debug("HTTP: Remote commit hash for '%s': %s", name, remote2)

	logger.Info("HTTP: Updating commit info for repository '%s' (local: %s, remote: %s)", name, local, remote2)
	if err := dbpkg.UpdateRepoCommits(s.db, name, local, remote2); err != nil {
		logger.Error("HTTP: Failed to update commit info for repository '%s': %v", name, err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}

	repo, _ = dbpkg.GetRepo(s.db, name)
	repo.MakeTarget = s.repoMakeTarget(name)
	latest, _ := dbpkg.GetLatestBuildRun(s.db, repo.ID)
	repo.LatestBuild = latest

	branches, _ := s.gitOps.ListLocalBranches(repo.LocalPath)
	logger.Info("HTTP: Refresh completed for repository '%s', found %d branches", name, len(branches))
	renderPartial(w, "repo_row.html", map[string]any{
		"Repo":     repo,
		"Branches": branches,
		"BasePath": s.basePath,
	})
}

func (s *Server) handleRefreshAll(w http.ResponseWriter, r *http.Request) {
	logger.Info("HTTP: %s %s - Refreshing all repositories", r.Method, r.URL.Path)

	repos, err := dbpkg.ListRepos(s.db)
	if err != nil {
		logger.Error("HTTP: Failed to list repositories: %v", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}

	logger.Info("HTTP: Refreshing %d repositories concurrently (concurrency: %d)", len(repos), s.cfg.Server.RefreshConcurrency)
	sem := make(chan struct{}, s.cfg.Server.RefreshConcurrency)
	var wg sync.WaitGroup
	for _, repo := range repos {
		wg.Add(1)
		go func(repo *models.Repository) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			remote := s.repoRemote(repo.Name)
			logger.Debug("HTTP: [concurrent] Fetching repository '%s'", repo.Name)
			if err := s.gitOps.Fetch(repo.LocalPath, remote); err != nil {
				logger.Warn("HTTP: [concurrent] Fetch failed for repository '%s': %v", repo.Name, err)
				return
			}
			local, err := s.gitOps.LocalCommitHash(repo.LocalPath, repo.ActiveBranch)
			if err != nil {
				logger.Warn("HTTP: [concurrent] Failed to get local commit for repository '%s': %v", repo.Name, err)
				return
			}
			remoteHash, err := s.gitOps.RemoteCommitHash(repo.LocalPath, remote, repo.ActiveBranch)
			if err != nil {
				logger.Warn("HTTP: [concurrent] Failed to get remote commit for repository '%s': %v", repo.Name, err)
				return
			}
			logger.Debug("HTTP: [concurrent] Updating commits for repository '%s' (local: %s, remote: %s)", repo.Name, local, remoteHash)
			if err := dbpkg.UpdateRepoCommits(s.db, repo.Name, local, remoteHash); err != nil {
				logger.Error("HTTP: [concurrent] Failed to update commits for repository '%s': %v", repo.Name, err)
			}
		}(repo)
	}
	wg.Wait()
	logger.Info("HTTP: All repository refreshes completed")

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
	logger.Info("HTTP: %s %s - Pulling repository '%s'", r.Method, r.URL.Path, name)

	repo, err := dbpkg.GetRepo(s.db, name)
	if err != nil {
		logger.Error("HTTP: Failed to get repository '%s': %v", name, err)
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	remote := s.repoRemote(name)
	logger.Info("HTTP: Pulling repository '%s' from remote '%s' branch '%s'", name, remote, repo.ActiveBranch)
	if err := s.gitOps.Pull(repo.LocalPath, remote, repo.ActiveBranch); err != nil {
		logger.Error("HTTP: Pull failed for repository '%s': %v", name, err)
		http.Error(w, fmt.Sprintf("pull failed: %v", err), http.StatusInternalServerError)
		return
	}
	logger.Info("HTTP: Pull completed for repository '%s'", name)

	logger.Debug("HTTP: Getting updated commit hashes for repository '%s'", name)
	local, err := s.gitOps.LocalCommitHash(repo.LocalPath, repo.ActiveBranch)
	if err != nil {
		logger.Error("HTTP: Failed to get local commit hash after pull for repository '%s': %v", name, err)
		http.Error(w, fmt.Sprintf("local hash: %v", err), http.StatusInternalServerError)
		return
	}
	remoteHash, err := s.gitOps.RemoteCommitHash(repo.LocalPath, remote, repo.ActiveBranch)
	if err != nil {
		logger.Error("HTTP: Failed to get remote commit hash after pull for repository '%s': %v", name, err)
		http.Error(w, fmt.Sprintf("remote hash: %v", err), http.StatusInternalServerError)
		return
	}

	logger.Info("HTTP: Updating commit info for repository '%s' after pull (local: %s, remote: %s)", name, local, remoteHash)
	if err := dbpkg.UpdateRepoCommits(s.db, name, local, remoteHash); err != nil {
		logger.Error("HTTP: Failed to update commit info for repository '%s' after pull: %v", name, err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}

	repo, _ = dbpkg.GetRepo(s.db, name)
	repo.MakeTarget = s.repoMakeTarget(name)
	latest, _ := dbpkg.GetLatestBuildRun(s.db, repo.ID)
	repo.LatestBuild = latest

	branches, _ := s.gitOps.ListLocalBranches(repo.LocalPath)
	logger.Info("HTTP: Pull operation completed for repository '%s'", name)
	renderPartial(w, "repo_row.html", map[string]any{
		"Repo":     repo,
		"Branches": branches,
		"BasePath": s.basePath,
	})
}

func (s *Server) handleBuild(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	logger.Info("HTTP: %s %s - Triggering build for repository '%s'", r.Method, r.URL.Path, name)

	repo, err := dbpkg.GetRepo(s.db, name)
	if err != nil {
		logger.Error("HTTP: Failed to get repository '%s': %v", name, err)
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	logger.Debug("HTTP: Checking for active builds for repository '%s' (ID: %d)", name, repo.ID)
	active, err := dbpkg.HasActiveBuild(s.db, repo.ID)
	if err != nil {
		logger.Error("HTTP: Failed to check for active builds for repository '%s': %v", name, err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	if active {
		logger.Warn("HTTP: Build already in progress for repository '%s', rejecting request", name)
		http.Error(w, "build already in progress", http.StatusConflict)
		return
	}

	makeTarget := s.repoMakeTarget(name)
	logger.Info("HTTP: Creating build run for repository '%s' (branch: %s, commit: %s, target: %s)", name, repo.ActiveBranch, repo.LocalCommit, makeTarget)
	buildID, err := dbpkg.InsertBuildRun(s.db, repo.ID, repo.ActiveBranch, repo.LocalCommit, makeTarget)
	if err != nil {
		logger.Error("HTTP: Failed to create build run for repository '%s': %v", name, err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	logger.Info("HTTP: Build run created with ID %d for repository '%s'", buildID, name)

	timeout := time.Duration(s.repoBuildTimeout(name)) * time.Second
	logger.Info("HTTP: Starting build %d for repository '%s' in background (timeout: %v)", buildID, name, timeout)
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
	logger.Debug("HTTP: %s %s - Checking build status for ID %s", r.Method, r.URL.Path, idStr)

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		logger.Warn("HTTP: Invalid build ID '%s': %v", idStr, err)
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	build, err := dbpkg.GetBuildRun(s.db, id)
	if err != nil {
		logger.Error("HTTP: Failed to get build run %d: %v", id, err)
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	logger.Debug("HTTP: Retrieved build status for run %d (repo: %s, status: %s)", id, build.RepoName, build.Status)
	renderPartial(w, "build_log.html", map[string]any{
		"Build":    build,
		"RepoName": r.PathValue("name"),
		"BasePath": s.basePath,
	})
}

func (s *Server) handleBranch(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	logger.Info("HTTP: %s %s - Changing branch for repository '%s'", r.Method, r.URL.Path, name)

	repo, err := dbpkg.GetRepo(s.db, name)
	if err != nil {
		logger.Error("HTTP: Failed to get repository '%s': %v", name, err)
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	if err := r.ParseForm(); err != nil {
		logger.Error("HTTP: Failed to parse form for repository '%s': %v", name, err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	branch := r.FormValue("branch")
	if branch == "" {
		logger.Warn("HTTP: Branch parameter required for repository '%s'", name)
		http.Error(w, "branch required", http.StatusBadRequest)
		return
	}

	logger.Info("HTTP: Updating repository '%s' branch from '%s' to '%s'", name, repo.ActiveBranch, branch)
	if err := dbpkg.UpdateRepoBranch(s.db, name, branch); err != nil {
		logger.Error("HTTP: Failed to update branch for repository '%s' to '%s': %v", name, branch, err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	logger.Info("HTTP: Branch updated for repository '%s' to '%s'", name, branch)

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
