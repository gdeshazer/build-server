package git

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/grantdeshazer/build-server/internal/logger"
)

const fetchTimeout = 30 * time.Second

func Fetch(repoPath, remote string) error {
	logger.Info("Executing git fetch: git -C %s fetch %s", repoPath, remote)

	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "fetch", remote)
	out, err := cmd.CombinedOutput()
	if err != nil {
		logger.Error("Git fetch failed: git -C %s fetch %s\nError: %v\nOutput:\n%s", repoPath, remote, err, string(out))
		return fmt.Errorf("git fetch: %w\n%s", err, out)
	}
	logger.Debug("Git fetch completed successfully: git -C %s fetch %s\nOutput:\n%s", repoPath, remote, string(out))
	return nil
}

func LocalCommitHash(repoPath, branch string) (string, error) {
	return revParse(repoPath, "refs/heads/"+branch)
}

func RemoteCommitHash(repoPath, remote, branch string) (string, error) {
	return revParse(repoPath, fmt.Sprintf("refs/remotes/%s/%s", remote, branch))
}

func revParse(repoPath, ref string) (string, error) {
	logger.Debug("Executing git rev-parse: git -C %s rev-parse %s", repoPath, ref)

	cmd := exec.Command("git", "-C", repoPath, "rev-parse", ref)
	out, err := cmd.Output()
	if err != nil {
		logger.Error("Git rev-parse failed: git -C %s rev-parse %s\nError: %v", repoPath, ref, err)
		return "", fmt.Errorf("git rev-parse %s: %w", ref, err)
	}
	result := strings.TrimSpace(string(out))
	logger.Debug("Git rev-parse completed: git -C %s rev-parse %s\nResult: %s", repoPath, ref, result)
	return result, nil
}

func Pull(repoPath, remote, branch string) error {
	logger.Info("Executing git pull: git -C %s pull %s %s", repoPath, remote, branch)

	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "pull", remote, branch)
	out, err := cmd.CombinedOutput()
	if err != nil {
		logger.Error("Git pull failed: git -C %s pull %s %s\nError: %v\nOutput:\n%s", repoPath, remote, branch, err, string(out))
		return fmt.Errorf("git pull: %w\n%s", err, out)
	}
	logger.Debug("Git pull completed successfully: git -C %s pull %s %s\nOutput:\n%s", repoPath, remote, branch, string(out))
	return nil
}

func ListLocalBranches(repoPath string) ([]string, error) {
	logger.Debug("Executing git branch: git -C %s branch --format=%%(refname:short)", repoPath)

	cmd := exec.Command("git", "-C", repoPath, "branch", "--format=%(refname:short)")
	out, err := cmd.Output()
	if err != nil {
		logger.Error("Git branch failed: git -C %s branch --format=%%(refname:short)\nError: %v", repoPath, err)
		return nil, fmt.Errorf("git branch: %w", err)
	}
	var branches []string
	for _, line := range bytes.Split(out, []byte("\n")) {
		b := strings.TrimSpace(string(line))
		if b != "" {
			branches = append(branches, b)
		}
	}
	logger.Debug("Git branch completed: git -C %s branch --format=%%(refname:short)\nFound %d branches: %v", repoPath, len(branches), branches)
	return branches, nil
}
