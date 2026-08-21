package main

import (
	"os"
	"runtime"
	"strconv"
)

// The one event this plugin subscribes to, and the state that makes it interesting: a
// task someone just reserved is a task about to be worked on.
const (
	eventStatusChanged = "task.status_changed"
	statusInProgress   = "in_progress"
)

// hook is the observation face: Amenbo fired an event and moved on.
//
// It only ever writes a suggestion to stderr. Creating the worktree here would be a side
// effect on a write path that cannot refuse it, on a decision this plugin is not in a
// position to make — whether THIS task wants its own checkout is a judgement, and the
// judgement belongs to the person or the AI that reads the suggestion. What the plugin
// can settle by itself is the fact underneath it: whether there is a git repository here
// at all. Where there is none, the suggestion would be noise, so nothing is written.
//
// It never fails. Nobody is waiting on the answer, and a non-zero exit would only put a
// warning in Amenbo's execution log for a run that had nothing to say.
func hook(in input) {
	if in.V != contractVersion {
		return
	}
	if in.Event != eventStatusChanged || in.New != statusInProgress {
		return
	}
	id := strconv.FormatInt(in.ID, 10)
	if _, _, worktree, err := paths(id); err == nil {
		if _, err := os.Stat(worktree); err == nil {
			logf("worktree: task %s already has a worktree: %s", id, worktree)
			return
		}
		logf("worktree: task %s is in_progress here, and this is a git repository.", id)
		logf(`  If it wants a checkout of its own:  eval "$(amenbo plugin run worktree start %s)"`, id)
		// The returned line is the same in either dialect; only the way to feed it to the
		// shell differs. On Windows either shell is plausible — PowerShell is the default
		// one, git-bash is what plenty of git users are standing in — so both are named
		// there. Elsewhere nobody is in PowerShell, and the second line would be noise.
		if runtime.GOOS == "windows" {
			logf(`                      in PowerShell:  iex (amenbo plugin run worktree start %s)`, id)
		}
	}
}
