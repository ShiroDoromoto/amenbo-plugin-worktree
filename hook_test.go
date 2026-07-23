package main

import (
	"os"
	"strings"
	"testing"
)

// reservation is the event amenbo fires when a task is reserved.
func reservation(id int64) input {
	return input{V: contractVersion, Event: eventStatusChanged, ID: id, New: statusInProgress}
}

// The hook advises and stops there: the suggestion is on stderr, the return value stays
// empty, and no worktree appears behind anyone's back.
func TestHookSuggestsTheCommandAndMakesNothing(t *testing.T) {
	setupRepo(t)
	stdout, stderr := capture(t)

	hook(reservation(123))

	if !strings.Contains(stderr.String(), "amenbo plugin run worktree start 123") {
		t.Errorf("the suggestion should name the command to run: %q", stderr)
	}
	if stdout.Len() != 0 {
		t.Errorf("a hook has no return value: %q", stdout)
	}
	_, worktreeBase, _, err := paths("123")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(worktreeBase); err == nil {
		t.Error("the hook must not create anything")
	}
}

// A task that already has its checkout does not need the suggestion; saying where it is
// answers the question the reservation actually raises.
func TestHookPointsAtAWorktreeThatAlreadyExists(t *testing.T) {
	setupRepo(t)
	_, stderr := capture(t)
	worktree := startOK(t, "123")
	stderr.Reset()

	hook(reservation(123))

	if !strings.Contains(stderr.String(), worktree) {
		t.Errorf("the existing worktree should be named: %q", stderr)
	}
	if strings.Contains(stderr.String(), "worktree start") {
		t.Errorf("and start should not be suggested for it: %q", stderr)
	}
}

// Outside a repository the suggestion would be noise. That much the plugin can settle by
// itself — it is a fact about the folder, not a judgement about the task.
func TestHookIsSilentOutsideAGitRepository(t *testing.T) {
	chdir(t, t.TempDir())
	_, stderr := capture(t)

	hook(reservation(123))

	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q — want silence", stderr)
	}
}

func TestHookIgnoresEveryOtherEventAndState(t *testing.T) {
	setupRepo(t)
	_, stderr := capture(t)

	for _, in := range []input{
		{V: contractVersion, Event: "task.created", ID: 123},
		{V: contractVersion, Event: "task.done", ID: 123},
		{V: contractVersion, Event: eventStatusChanged, ID: 123, New: "blocked"},
		{V: contractVersion, Event: eventStatusChanged, ID: 123, New: "todo"},
	} {
		hook(in)
		if stderr.Len() != 0 {
			t.Fatalf("%+v said %q — want silence", in, stderr)
		}
	}
}

// A document announcing another contract version is one this plugin cannot read: `v`
// moves only when an existing field's meaning breaks, so guessing would be acting on a
// field that no longer means what the code below assumes.
func TestHookIgnoresAContractItDoesNotRead(t *testing.T) {
	setupRepo(t)
	_, stderr := capture(t)

	in := reservation(123)
	in.V = contractVersion + 1
	hook(in)

	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q — want silence", stderr)
	}
}
