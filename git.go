package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// git runs one git command in dir and returns its trimmed stdout. A failure carries the
// captured stderr, so a refusal can name git's own reason instead of "exit status 128".
func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = strings.TrimSpace(stdout.String())
		}
		return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, message)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// gitRoot returns the MAIN worktree root of the repo containing dir — not the linked
// worktree one might be standing in. That distinction is what lets the plugin behave the
// same from either place: --show-toplevel would answer with the task worktree itself,
// and the sibling layout would then resolve one level deeper every time. --git-common-dir
// resolves to the main repo's .git, whose parent is the main worktree root.
func gitRoot(dir string) (string, error) {
	common, err := git(dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", err
	}
	return filepath.Dir(common), nil
}

// worktreeAdd checks a new worktree out at path, on a fresh branch cut from base.
func worktreeAdd(root, path, branch, base string) error {
	_, err := git(root, "worktree", "add", path, "-b", branch, base)
	return err
}

// worktreeRemove detaches the worktree at path. force discards uncommitted changes.
func worktreeRemove(root, path string, force bool) error {
	args := []string{"worktree", "remove", path}
	if force {
		args = append(args, "--force")
	}
	_, err := git(root, args...)
	return err
}

// branchExists reports whether refs/heads/branch is present.
func branchExists(root, branch string) bool {
	_, err := git(root, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}

// branchDelete deletes branch. force allows deleting one that is not merged.
func branchDelete(root, branch string, force bool) error {
	flag := "-d"
	if force {
		flag = "-D"
	}
	_, err := git(root, "branch", flag, branch)
	return err
}

// isClean reports whether the worktree at path has no uncommitted changes.
func isClean(path string) (bool, error) {
	status, err := git(path, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return status == "", nil
}

// isMerged reports whether branch is fully contained in base — its tip is an ancestor of
// base, so deleting it loses nothing. --is-ancestor answers "no" by exiting 1, which is
// an answer rather than a failure, so anything non-zero reads as not merged.
func isMerged(root, branch, base string) bool {
	_, err := git(root, "merge-base", "--is-ancestor", branch, base)
	return err == nil
}
