package db

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/grantdeshazer/build-server/internal/models"
)

func ListRepos(db *sql.DB) ([]*models.Repository, error) {
	rows, err := db.Query(`
		SELECT id, name, local_path, remote_url, active_branch,
		       COALESCE(local_commit, ''), COALESCE(remote_commit, ''),
		       last_refreshed, active, created_at, updated_at
		FROM repositories
		WHERE active = 1
		ORDER BY name
	`)
	if err != nil {
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
			return nil, err
		}
		if lastRefreshed.Valid {
			t, _ := time.Parse("2006-01-02 15:04:05", lastRefreshed.String)
			r.LastRefreshed = &t
		}
		repos = append(repos, r)
	}
	return repos, rows.Err()
}

func GetRepo(db *sql.DB, name string) (*models.Repository, error) {
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
		return nil, fmt.Errorf("repo %q not found", name)
	}
	if err != nil {
		return nil, err
	}
	if lastRefreshed.Valid {
		t, _ := time.Parse("2006-01-02 15:04:05", lastRefreshed.String)
		r.LastRefreshed = &t
	}
	return r, nil
}

func UpdateRepoCommits(db *sql.DB, name, localCommit, remoteCommit string) error {
	_, err := db.Exec(`
		UPDATE repositories SET
			local_commit = ?,
			remote_commit = ?,
			last_refreshed = datetime('now'),
			updated_at = datetime('now')
		WHERE name = ?
	`, localCommit, remoteCommit, name)
	return err
}

func UpdateRepoBranch(db *sql.DB, name, branch string) error {
	_, err := db.Exec(`
		UPDATE repositories SET
			active_branch = ?,
			local_commit = NULL,
			remote_commit = NULL,
			last_refreshed = NULL,
			updated_at = datetime('now')
		WHERE name = ?
	`, branch, name)
	return err
}

func HasActiveBuild(db *sql.DB, repoID int64) (bool, error) {
	var count int
	err := db.QueryRow(`
		SELECT COUNT(*) FROM build_runs
		WHERE repo_id = ? AND status IN ('pending', 'running')
	`, repoID).Scan(&count)
	return count > 0, err
}

func InsertBuildRun(db *sql.DB, repoID int64, branch, commitHash, makeTarget string) (int64, error) {
	res, err := db.Exec(`
		INSERT INTO build_runs (repo_id, branch, commit_hash, make_target, status)
		VALUES (?, ?, ?, ?, 'pending')
	`, repoID, branch, commitHash, makeTarget)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func UpdateBuildRunStart(db *sql.DB, id int64) error {
	_, err := db.Exec(`
		UPDATE build_runs SET status = 'running', started_at = datetime('now')
		WHERE id = ?
	`, id)
	return err
}

func UpdateBuildRunFinish(db *sql.DB, id int64, status string, exitCode int, logOutput string) error {
	const maxLog = 512 * 1024
	if len(logOutput) > maxLog {
		logOutput = logOutput[:maxLog]
	}
	_, err := db.Exec(`
		UPDATE build_runs SET
			status = ?,
			exit_code = ?,
			log_output = ?,
			finished_at = datetime('now')
		WHERE id = ?
	`, status, exitCode, logOutput, id)
	return err
}

func GetBuildRun(db *sql.DB, id int64) (*models.BuildRun, error) {
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
		return nil, fmt.Errorf("build run %d not found", id)
	}
	if err != nil {
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
	return b, nil
}

func GetLatestBuildRun(db *sql.DB, repoID int64) (*models.BuildRun, error) {
	var id int64
	err := db.QueryRow(`
		SELECT id FROM build_runs WHERE repo_id = ? ORDER BY id DESC LIMIT 1
	`, repoID).Scan(&id)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return GetBuildRun(db, id)
}
