package models

import "time"

type Repository struct {
	ID            int64
	Name          string
	LocalPath     string
	RemoteURL     string
	ActiveBranch  string
	LocalCommit   string
	RemoteCommit  string
	LastRefreshed *time.Time
	Active        bool
	CreatedAt     time.Time
	UpdatedAt     time.Time

	// Derived fields for display
	MakeTarget    string
	LatestBuild   *BuildRun
}

func (r *Repository) CommitStatus() string {
	if r.LocalCommit == "" || r.RemoteCommit == "" {
		return "unknown"
	}
	if r.LocalCommit == r.RemoteCommit {
		return "up-to-date"
	}
	return "diverged"
}

func (r *Repository) ShortLocal() string {
	return shortHash(r.LocalCommit)
}

func (r *Repository) ShortRemote() string {
	return shortHash(r.RemoteCommit)
}

func shortHash(h string) string {
	if len(h) >= 7 {
		return h[:7]
	}
	return h
}

type BuildRun struct {
	ID         int64
	RepoID     int64
	RepoName   string
	Branch     string
	CommitHash string
	MakeTarget string
	Status     string
	ExitCode   *int
	LogOutput  string
	StartedAt  *time.Time
	FinishedAt *time.Time
	CreatedAt  time.Time
}

func (b *BuildRun) IsTerminal() bool {
	return b.Status == "success" || b.Status == "failed" || b.Status == "cancelled"
}

func (b *BuildRun) ShortCommit() string {
	return shortHash(b.CommitHash)
}
