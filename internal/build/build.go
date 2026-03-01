package build

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	dbpkg "github.com/grantdeshazer/build-server/internal/db"
	"github.com/grantdeshazer/build-server/internal/logger"
)

func Run(db *sql.DB, buildID int64, repoPath, makeTarget string, timeout time.Duration) {
	logger.Info("Build %d: Starting build with target '%s' in %s (timeout: %v)", buildID, makeTarget, repoPath, timeout)

	logger.Info("Build %d: Updating build status to 'running'", buildID)
	if err := dbpkg.UpdateBuildRunStart(db, buildID); err != nil {
		logger.Error("Build %d: Failed to update build start status: %v", buildID, err)
		return
	}
	logger.Info("Build %d: Build status updated to 'running'", buildID)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	logger.Info("Build %d: Executing command: make %s (in directory: %s)", buildID, makeTarget, repoPath)
	cmd := exec.CommandContext(ctx, "make", makeTarget)
	cmd.Dir = repoPath

	var buf bytes.Buffer
	mw := io.MultiWriter(&buf, os.Stdout)
	cmd.Stdout = mw
	cmd.Stderr = mw

	err := cmd.Run()
	output := buf.String()

	exitCode := 0
	status := "success"
	if err != nil {
		status = "failed"
		exitCode = -1
		if ctx.Err() == context.DeadlineExceeded {
			logger.Error("Build %d: Build timed out after %v", buildID, timeout)
			fmt.Fprintf(&buf, "\n[build-server] build timed out after %v\n", timeout)
			output = buf.String()
		} else if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
			logger.Error("Build %d: Build failed with exit code %d", buildID, exitCode)
		} else {
			logger.Error("Build %d: Build failed with error: %v", buildID, err)
		}
		logger.Error("Build %d: Full build output:\n%s", buildID, output)
	} else {
		logger.Info("Build %d: Build completed successfully with exit code 0", buildID)
		logger.Debug("Build %d: Build output:\n%s", buildID, output)
	}

	logger.Info("Build %d: Updating build status to '%s' with exit code %d", buildID, status, exitCode)
	if err := dbpkg.UpdateBuildRunFinish(db, buildID, status, exitCode, output); err != nil {
		logger.Error("Build %d: Failed to update build finish status: %v", buildID, err)
	} else {
		logger.Info("Build %d: Build finish status updated successfully", buildID)
	}
}
