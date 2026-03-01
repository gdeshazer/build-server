package git

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const fetchTimeout = 30 * time.Second

func Fetch(repoPath, remote string) error {
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "fetch", remote)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git fetch: %w\n%s", err, out)
	}
	return nil
}

func LocalCommitHash(repoPath, branch string) (string, error) {
	return revParse(repoPath, "refs/heads/"+branch)
}

func RemoteCommitHash(repoPath, remote, branch string) (string, error) {
	return revParse(repoPath, fmt.Sprintf("refs/remotes/%s/%s", remote, branch))
}

func revParse(repoPath, ref string) (string, error) {
	cmd := exec.Command("git", "-C", repoPath, "rev-parse", ref)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse %s: %w", ref, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func ListLocalBranches(repoPath string) ([]string, error) {
	cmd := exec.Command("git", "-C", repoPath, "branch", "--format=%(refname:short)")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git branch: %w", err)
	}
	var branches []string
	for _, line := range bytes.Split(out, []byte("\n")) {
		b := strings.TrimSpace(string(line))
		if b != "" {
			branches = append(branches, b)
		}
	}
	return branches, nil
}
