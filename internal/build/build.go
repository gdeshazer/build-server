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
)

func Run(db *sql.DB, buildID int64, repoPath, makeTarget string, timeout time.Duration) {
	if err := dbpkg.UpdateBuildRunStart(db, buildID); err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

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
			fmt.Fprintf(&buf, "\n[build-server] build timed out after %v\n", timeout)
			output = buf.String()
		} else if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}

	dbpkg.UpdateBuildRunFinish(db, buildID, status, exitCode, output)
}
