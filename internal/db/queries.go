package db

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/grantdeshazer/build-server/internal/logger"
	"github.com/grantdeshazer/build-server/internal/models"
)

func ListRepos(db *sql.DB) ([]*models.Repository, error) {
	logger.Debug("DB: Executing query: SELECT id, name, local_path, remote_url, active_branch, local_commit, remote_commit, last_refreshed, active, created_at, updated_at FROM repositories WHERE active = 1 ORDER BY name")

	rows, err := db.Query(`
		SELECT id, name, local_path, remote_url, active_branch,
		       COALESCE(local_commit, ''), COALESCE(remote_commit, ''),
		       last_refreshed, active, created_at, updated_at
		FROM repositories
		WHERE active = 1
		ORDER BY name
	`)
	if err != nil {
		logger.Error("DB: Failed to list repositories: %v", err)
		return nil, err
	}
	defer rows.Close()

	var repos []*models.Repository
	for rows.Next() {
		r := &models.Repository{}
		var lastRefreshed sql.NullString
		var createdAt, updatedAt string
		err := rows.Scan(
			&r.ID, &r.Name, &r.LocalPath, &r.RemoteURL, &r.ActiveBranch,
			&r.LocalCommit, &r.RemoteCommit,
			&lastRefreshed, &r.Active, &createdAt, &updatedAt,
		)
		if err != nil {
			logger.Error("DB: Failed to scan repository row: %v", err)
			return nil, err
		}
		if lastRefreshed.Valid {
			t, _ := time.Parse("2006-01-02 15:04:05", lastRefreshed.String)
			r.LastRefreshed = &t
		}
		repos = append(repos, r)
	}

	if err := rows.Err(); err != nil {
		logger.Error("DB: Error iterating repository rows: %v", err)
		return nil, err
	}

	logger.Info("DB: Successfully listed %d active repositories", len(repos))
	return repos, nil
}

func GetRepo(db *sql.DB, name string) (*models.Repository, error) {
	logger.Debug("DB: Executing query: SELECT id, name, local_path, remote_url, active_branch, local_commit, remote_commit, last_refreshed, active, created_at, updated_at FROM repositories WHERE name = '%s' AND active = 1", name)

	r := &models.Repository{}
	var lastRefreshed sql.NullString
	var createdAt, updatedAt string
	err := db.QueryRow(`
		SELECT id, name, local_path, remote_url, active_branch,
		       COALESCE(local_commit, ''), COALESCE(remote_commit, ''),
		       last_refreshed, active, created_at, updated_at
		FROM repositories WHERE name = ? AND active = 1
	`, name).Scan(
		&r.ID, &r.Name, &r.LocalPath, &r.RemoteURL, &r.ActiveBranch,
		&r.LocalCommit, &r.RemoteCommit,
		&lastRefreshed, &r.Active, &createdAt, &updatedAt,
	)
	if err == sql.ErrNoRows {
		logger.Warn("DB: Repository '%s' not found", name)
		return nil, fmt.Errorf("repo %q not found", name)
	}
	if err != nil {
		logger.Error("DB: Failed to get repository '%s': %v", name, err)
		return nil, err
	}
	if lastRefreshed.Valid {
		t, _ := time.Parse("2006-01-02 15:04:05", lastRefreshed.String)
		r.LastRefreshed = &t
	}

	logger.Info("DB: Successfully retrieved repository '%s' (ID: %d)", name, r.ID)
	return r, nil
}

func UpdateRepoCommits(db *sql.DB, name, localCommit, remoteCommit string) error {
	logger.Debug("DB: Executing query: UPDATE repositories SET local_commit = '%s', remote_commit = '%s', last_refreshed = datetime('now'), updated_at = datetime('now') WHERE name = '%s'", localCommit, remoteCommit, name)

	result, err := db.Exec(`
		UPDATE repositories SET
			local_commit = ?,
			remote_commit = ?,
			last_refreshed = datetime('now'),
			updated_at = datetime('now')
		WHERE name = ?
	`, localCommit, remoteCommit, name)
	if err != nil {
		logger.Error("DB: Failed to update repository commits for '%s': %v", name, err)
		return err
	}

	rowsAffected, _ := result.RowsAffected()
	logger.Info("DB: Successfully updated repository commits for '%s' (local: %s, remote: %s, rows affected: %d)", name, localCommit, remoteCommit, rowsAffected)
	return nil
}

func UpdateRepoBranch(db *sql.DB, name, branch string) error {
	logger.Debug("DB: Executing query: UPDATE repositories SET active_branch = '%s', local_commit = NULL, remote_commit = NULL, last_refreshed = NULL, updated_at = datetime('now') WHERE name = '%s'", branch, name)

	result, err := db.Exec(`
		UPDATE repositories SET
			active_branch = ?,
			local_commit = NULL,
			remote_commit = NULL,
			last_refreshed = NULL,
			updated_at = datetime('now')
		WHERE name = ?
	`, branch, name)
	if err != nil {
		logger.Error("DB: Failed to update repository branch for '%s' to '%s': %v", name, branch, err)
		return err
	}

	rowsAffected, _ := result.RowsAffected()
	logger.Info("DB: Successfully updated repository branch for '%s' to '%s' (rows affected: %d)", name, branch, rowsAffected)
	return nil
}

func HasActiveBuild(db *sql.DB, repoID int64) (bool, error) {
	logger.Debug("DB: Executing query: SELECT COUNT(*) FROM build_runs WHERE repo_id = %d AND status IN ('pending', 'running')", repoID)

	var count int
	err := db.QueryRow(`
		SELECT COUNT(*) FROM build_runs
		WHERE repo_id = ? AND status IN ('pending', 'running')
	`, repoID).Scan(&count)
	if err != nil {
		logger.Error("DB: Failed to check for active builds for repo ID %d: %v", repoID, err)
		return false, err
	}

	hasActive := count > 0
	if hasActive {
		logger.Info("DB: Found %d active build(s) for repository ID %d", count, repoID)
	} else {
		logger.Debug("DB: No active builds found for repository ID %d", repoID)
	}
	return hasActive, nil
}

func InsertBuildRun(db *sql.DB, repoID int64, branch, commitHash, makeTarget string) (int64, error) {
	logger.Debug("DB: Executing query: INSERT INTO build_runs (repo_id, branch, commit_hash, make_target, status) VALUES (%d, '%s', '%s', '%s', 'pending')", repoID, branch, commitHash, makeTarget)

	res, err := db.Exec(`
		INSERT INTO build_runs (repo_id, branch, commit_hash, make_target, status)
		VALUES (?, ?, ?, ?, 'pending')
	`, repoID, branch, commitHash, makeTarget)
	if err != nil {
		logger.Error("DB: Failed to insert build run for repo ID %d (branch: %s, target: %s): %v", repoID, branch, makeTarget, err)
		return 0, err
	}

	buildID, err := res.LastInsertId()
	if err != nil {
		logger.Error("DB: Failed to get last insert ID for build run: %v", err)
		return 0, err
	}

	logger.Info("DB: Successfully inserted build run ID %d for repo ID %d (branch: %s, target: %s)", buildID, repoID, branch, makeTarget)
	return buildID, nil
}

func UpdateBuildRunStart(db *sql.DB, id int64) error {
	logger.Debug("DB: Executing query: UPDATE build_runs SET status = 'running', started_at = datetime('now') WHERE id = %d", id)

	result, err := db.Exec(`
		UPDATE build_runs SET status = 'running', started_at = datetime('now')
		WHERE id = ?
	`, id)
	if err != nil {
		logger.Error("DB: Failed to update build run %d start status: %v", id, err)
		return err
	}

	rowsAffected, _ := result.RowsAffected()
	logger.Info("DB: Successfully updated build run %d status to 'running' (rows affected: %d)", id, rowsAffected)
	return nil
}

func UpdateBuildRunFinish(db *sql.DB, id int64, status string, exitCode int, logOutput string) error {
	const maxLog = 512 * 1024
	outputLen := len(logOutput)
	if outputLen > maxLog {
		logOutput = logOutput[:maxLog]
		logger.Warn("DB: Truncating build output for build run %d from %d to %d bytes", id, outputLen, maxLog)
	}

	logger.Debug("DB: Executing query: UPDATE build_runs SET status = '%s', exit_code = %d, log_output = '<%d bytes>', finished_at = datetime('now') WHERE id = %d", status, exitCode, len(logOutput), id)

	result, err := db.Exec(`
		UPDATE build_runs SET
			status = ?,
			exit_code = ?,
			log_output = ?,
			finished_at = datetime('now')
		WHERE id = ?
	`, status, exitCode, logOutput, id)
	if err != nil {
		logger.Error("DB: Failed to update build run %d finish status (status: %s, exit_code: %d): %v", id, status, exitCode, err)
		return err
	}

	rowsAffected, _ := result.RowsAffected()
	logger.Info("DB: Successfully updated build run %d finish status (status: %s, exit_code: %d, rows affected: %d)", id, status, exitCode, rowsAffected)
	return nil
}

func GetBuildRun(db *sql.DB, id int64) (*models.BuildRun, error) {
	logger.Debug("DB: Executing query: SELECT br.id, br.repo_id, r.name, br.branch, br.commit_hash, br.make_target, br.status, br.exit_code, br.log_output, br.started_at, br.finished_at, br.created_at FROM build_runs br JOIN repositories r ON r.id = br.repo_id WHERE br.id = %d", id)

	b := &models.BuildRun{}
	var startedAt, finishedAt sql.NullString
	var createdAt string
	var exitCode sql.NullInt64
	var logOutput sql.NullString
	var commitHash sql.NullString

	err := db.QueryRow(`
		SELECT br.id, br.repo_id, r.name, br.branch, COALESCE(br.commit_hash, ''),
		       br.make_target, br.status, br.exit_code, br.log_output,
		       br.started_at, br.finished_at, br.created_at
		FROM build_runs br
		JOIN repositories r ON r.id = br.repo_id
		WHERE br.id = ?
	`, id).Scan(
		&b.ID, &b.RepoID, &b.RepoName, &b.Branch, &commitHash,
		&b.MakeTarget, &b.Status, &exitCode, &logOutput,
		&startedAt, &finishedAt, &createdAt,
	)
	if err == sql.ErrNoRows {
		logger.Warn("DB: Build run %d not found", id)
		return nil, fmt.Errorf("build run %d not found", id)
	}
	if err != nil {
		logger.Error("DB: Failed to get build run %d: %v", id, err)
		return nil, err
	}
	if commitHash.Valid {
		b.CommitHash = commitHash.String
	}
	if exitCode.Valid {
		v := int(exitCode.Int64)
		b.ExitCode = &v
	}
	if logOutput.Valid {
		b.LogOutput = logOutput.String
	}
	if startedAt.Valid {
		t, _ := time.Parse("2006-01-02 15:04:05", startedAt.String)
		b.StartedAt = &t
	}
	if finishedAt.Valid {
		t, _ := time.Parse("2006-01-02 15:04:05", finishedAt.String)
		b.FinishedAt = &t
	}

	logger.Info("DB: Successfully retrieved build run %d (repo: %s, status: %s)", id, b.RepoName, b.Status)
	return b, nil
}

func GetLatestBuildRun(db *sql.DB, repoID int64) (*models.BuildRun, error) {
	logger.Debug("DB: Executing query: SELECT id FROM build_runs WHERE repo_id = %d ORDER BY id DESC LIMIT 1", repoID)

	var id int64
	err := db.QueryRow(`
		SELECT id FROM build_runs WHERE repo_id = ? ORDER BY id DESC LIMIT 1
	`, repoID).Scan(&id)
	if err == sql.ErrNoRows {
		logger.Debug("DB: No build runs found for repository ID %d", repoID)
		return nil, nil
	}
	if err != nil {
		logger.Error("DB: Failed to get latest build run for repo ID %d: %v", repoID, err)
		return nil, err
	}

	logger.Debug("DB: Found latest build run ID %d for repository ID %d", id, repoID)
	return GetBuildRun(db, id)
}
