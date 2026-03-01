package build

import (
	"context"
	"database/sql"
	"errors"
	"os/exec"
	"time"

	dbpkg "github.com/grantdeshazer/build-server/internal/db"
)

const buildTimeout = 30 * time.Minute

func Run(db *sql.DB, buildID int64, repoPath, makeTarget string) {
	if err := dbpkg.UpdateBuildRunStart(db, buildID); err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), buildTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "make", makeTarget)
	cmd.Dir = repoPath

	out, err := cmd.CombinedOutput()

	exitCode := 0
	status := "success"
	if err != nil {
		status = "failed"
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	dbpkg.UpdateBuildRunFinish(db, buildID, status, exitCode, string(out))
}
