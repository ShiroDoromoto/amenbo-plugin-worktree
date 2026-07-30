package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// branchName is the branch one task's worktree is checked out on.
func branchName(id string) string { return "task/" + id }

// paths resolves the main repo root, the sibling dir holding every task's worktree, and
// this task's worktree inside it. They are derived from the current directory, so the
// commands must be invoked from the repository the task belongs to — which is also the
// only place amenbo can find the project this plugin is enabled for.
func paths(id string) (root, base, worktree string, err error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", "", "", err
	}
	root, err = gitRoot(cwd)
	if err != nil {
		return "", "", "", fmt.Errorf("not inside a git repository: %w", err)
	}
	base = filepath.Join(filepath.Dir(root), filepath.Base(root)+"-worktrees")
	return root, base, filepath.Join(base, id), nil
}

// start gives task id a worktree of its own and returns the way into it.
//
// The return value on stdout is one `cd` line, so the caller enters the checkout with
// `eval "$(amenbo plugin run worktree start <id>)"` — `iex (amenbo plugin run worktree
// start <id>)` in PowerShell, the same line either way; everything a human reads goes to
// stderr beside it. The backlog is not touched: amenbo already moved the task to
// in_progress — that reservation is what fired the hook that suggested this command.
func start(id, base string) error {
	root, worktreeBase, worktree, err := paths(id)
	if err != nil {
		return err
	}
	// Settled before anything is created: the way in is the whole return value, and a
	// checkout on disk that no returned line can enter is worse than a refusal.
	cd, err := shellQuote(worktree)
	if err != nil {
		return err
	}
	if _, err := os.Stat(worktree); err == nil {
		return existingWorktree(id, worktree)
	}
	if branchExists(root, branchName(id)) {
		return fmt.Errorf("branch %s already exists — finish the task's worktree, or delete the branch, before starting again", branchName(id))
	}
	if err := os.MkdirAll(worktreeBase, 0o755); err != nil {
		return err
	}
	if err := worktreeAdd(root, worktree, branchName(id), base); err != nil {
		return fmt.Errorf("worktree add: %w", err)
	}

	logf("✓ task %s has a worktree: %s  (branch %s, cut from %s)", id, worktree, branchName(id), base)
	logf("  code, build and test there — it is a development environment, not a bound folder")
	logf("  backlog moves (comment / done) stay with amenbo, run from %s", root)
	fmt.Fprintf(out, "cd %s\n", cd)
	return nil
}

// existingWorktree is the refusal a second start meets, and it is written to be read by
// whoever is about to reach for the dir anyway. The two accidents it can be — another
// session at work, or one's own leftover — look identical on disk, and this plugin
// cannot tell them apart: the backlog is amenbo's, and reading it is not a git
// operation. So the refusal names both and points at the one place that settles it.
func existingWorktree(id, worktree string) error {
	return fmt.Errorf(`%s already exists for task %s.

  ANOTHER SESSION MAY BE WORKING THERE. Do not look inside it, do not judge whether it
  is stale, do not delete it. Ask the backlog first: `+"`amenbo task show %s`"+` — in_progress
  means someone is on it, and the answer is to take a different task.

  Only if you know the worktree is your own leftover:
  `+"`amenbo plugin run worktree finish %s`"+`.`, worktree, id, id, id)
}

// finish takes the worktree and its branch away again.
//
// It refuses while there is anything left to lose: uncommitted work in the checkout, or
// a branch whose commits have not reached base. --force overrides both, which is the
// only way to discard work on purpose. There is no return value — a teardown answers
// nothing — so stdout stays empty and the account of what happened is on stderr.
func finish(id, base string, force bool) error {
	root, worktreeBase, worktree, err := paths(id)
	if err != nil {
		return err
	}
	if _, err := os.Stat(worktree); err != nil {
		return fmt.Errorf("task %s has no worktree (%s is missing)", id, worktree)
	}

	if !force {
		clean, err := isClean(worktree)
		if err != nil {
			return fmt.Errorf("reading the worktree's state: %w (--force tears it down regardless)", err)
		}
		if !clean {
			return fmt.Errorf("worktree %s has uncommitted changes — commit them, or use --force to discard them", worktree)
		}
		if !isMerged(root, branchName(id), base) {
			return fmt.Errorf("branch %s has commits %s does not — merge it, or use --force to discard them", branchName(id), base)
		}
	}

	if err := worktreeRemove(root, worktree, force); err != nil {
		return fmt.Errorf("worktree remove: %w", err)
	}
	if err := branchDelete(root, branchName(id), force); err != nil {
		return fmt.Errorf("branch delete: %w", err)
	}
	// `git worktree remove` deletes the dir, but a leftover is swept anyway, and the
	// sibling base dir is pruned once it is empty. A non-empty error there means another
	// task's worktree still lives in it, so it is left alone.
	if err := os.RemoveAll(worktree); err != nil {
		return fmt.Errorf("remove %s: %w", worktree, err)
	}
	_ = os.Remove(worktreeBase)

	logf("✓ task %s torn down (worktree and branch %s removed)", id, branchName(id))
	logf("  the backlog is untouched — close the task with `amenbo task done %s`, or hand it back with `amenbo task status %s todo`", id, id)
	return nil
}

// shellQuote single-quotes a path so the returned `cd` line survives being fed to a shell —
// `eval "$(…)"` in a POSIX one, `iex (…)` in PowerShell. One line has to satisfy both, since
// what a plugin returns is passed through untouched and nobody downstream knows the dialect.
//
// Single quotes are the form the two share: everything between them is literal in both, which
// is what carries a Windows path's backslashes through unharmed (double quotes would not —
// POSIX reads a backslash as an escape). What they do not share is how to put a single quote
// *inside*: POSIX ends the string and re-opens it, PowerShell doubles the quote, and neither
// spelling is inert in the other. So a path carrying one has no line that works in both, and
// this refuses rather than return one that works where it was baked and breaks elsewhere.
func shellQuote(s string) (string, error) {
	if strings.Contains(s, "'") {
		return "", fmt.Errorf("the path %s contains a single quote, which cannot be quoted in a way both a POSIX shell and PowerShell read alike — move the repository somewhere without one, and the `cd` line works in either", s)
	}
	return "'" + s + "'", nil
}
