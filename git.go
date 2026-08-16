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

// headName is the branch the main repo is standing on. A detached HEAD has no branch to
// name, so it answers with the word git itself resolves — which cuts and measures from
// the same commit, and reads as what it is wherever it is printed.
func headName(root string) string {
	name, err := git(root, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil || name == "" {
		return "HEAD"
	}
	return name
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

// branchDelete deletes branch without asking git whether anything is lost by it. Reaching
// a delete at all means that question is already settled — either isMerged found the
// branch's changes in base, or the caller said to discard them on purpose — and git's own
// answer is the weaker of the two: it measures lineage, so it refuses a branch whose
// changes were squashed or rebased into base as readily as one that was never merged.
func branchDelete(root, branch string) error {
	_, err := git(root, "branch", "-D", branch)
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

// isMerged reports whether every change on branch is already in base, so deleting it loses
// nothing. The measure is the patch each commit carries rather than where the commit sits
// in the graph, because a plugin is a distributable and the way its users merge is not its
// to choose: a squash and a rebase merge both land the branch's changes under commits of
// their own, and lineage would call those unmerged forever, leaving --force as the only way
// to tear a finished task down. `git cherry` marks a commit `-` when base already holds an
// equivalent patch and `+` when it does not, and leaves merge commits out of the comparison,
// so a branch that took base in along the way is not measured against what it took.
//
// A patch that changed on its way in — a conflict resolved by hand — has no equivalent to
// find, and the answer falls on the refusing side. That is the safe way to be wrong: the
// work is untouched, and saying so deliberately is what --force is for.
func isMerged(root, branch, base string) bool {
	cherry, err := git(root, "cherry", base, branch)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(cherry, "\n") {
		if strings.HasPrefix(line, "+") {
			return false
		}
	}
	return true
}
