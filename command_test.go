package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// startOK runs start and fails the test if it refuses, returning the worktree path.
func startOK(t *testing.T, id string) string {
	t.Helper()
	if err := start(id, ""); err != nil {
		t.Fatalf("start %s: %v", id, err)
	}
	_, _, worktree, err := paths(id)
	if err != nil {
		t.Fatal(err)
	}
	return worktree
}

// The return value is the `cd` a caller evals, and only that — everything else is on
// stderr. A summary leaking into stdout would be evaluated as a shell command.
func TestStartReturnsOnlyTheCdAndKeepsTheSummaryOnStderr(t *testing.T) {
	root := setupRepo(t)
	stdout, stderr := capture(t)

	worktree := startOK(t, "123")

	cd, err := shellQuote(worktree)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := stdout.String(), "cd "+cd+"\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if !strings.Contains(stderr.String(), "task/123") {
		t.Errorf("the summary names the branch: %q", stderr)
	}
	if _, err := os.Stat(filepath.Join(worktree, ".git")); err != nil {
		t.Errorf("the worktree should be checked out: %v", err)
	}
	if !branchExists(root, "task/123") {
		t.Error("the branch should exist")
	}
	// The worktree is a sibling of the repo, not a child: nothing inside the repo can
	// mistake it for part of the project.
	repoRoot, worktreeBase, _, err := paths("123")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := filepath.Dir(worktreeBase), filepath.Dir(repoRoot); got != want {
		t.Errorf("the worktrees dir sits in %s, want beside the repo in %s", got, want)
	}
}

// A worktree that is already there is the signal that someone may be working in it, so
// the second start refuses rather than reusing or replacing it.
func TestStartRefusesAWorktreeThatIsAlreadyThere(t *testing.T) {
	setupRepo(t)
	capture(t)
	startOK(t, "123")

	err := start("123", "")
	if err == nil {
		t.Fatal("the second start should refuse")
	}
	if !strings.Contains(err.Error(), "amenbo task show 123") {
		t.Errorf("the refusal should point at the backlog, which settles it: %v", err)
	}
}

// The branch is the other half of the same identity: it can outlive a worktree that was
// removed by hand, and reusing it would put two tasks' commits on one branch.
func TestStartRefusesWhenTheBranchSurvivedTheWorktree(t *testing.T) {
	root := setupRepo(t)
	capture(t)
	worktree := startOK(t, "123")
	if err := worktreeRemove(root, worktree, true); err != nil {
		t.Fatal(err)
	}

	err := start("123", "")
	if err == nil || !strings.Contains(err.Error(), "task/123") {
		t.Fatalf("start = %v — want a refusal naming the branch", err)
	}
}

// A worktree takes its identity from where the command was typed, so the same start typed
// in the wrong repository succeeds and hands back a checkout of a different project. Where
// the task names the folder it is worked in, there is an answer to hold it to.
func TestStartRefusesARepositoryTheTaskIsNotWorkedIn(t *testing.T) {
	setupRepo(t)
	stdout, _ := capture(t)
	other := t.TempDir()
	if _, err := git(other, "init", "-b", "main"); err != nil {
		t.Fatal(err)
	}
	taskWorkedIn(t, other)

	err := start("123", "")
	if err == nil {
		t.Fatal("a start in a repository the task is not worked in should refuse")
	}
	if !strings.Contains(err.Error(), other) {
		t.Errorf("the refusal should name the folder to start in: %v", err)
	}
	if stdout.Len() != 0 {
		t.Errorf("a refused start returns nothing, stdout = %q", stdout)
	}
	_, _, worktree, err := paths("123")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(worktree); !os.IsNotExist(err) {
		t.Errorf("a refused start creates nothing, stat err = %v", err)
	}
}

// The folder a task names is one its project is bound to, which need not be the repository
// root — so what is compared is the repository the folder lies in, not the folder itself.
func TestStartCutsWhereTheTaskNamesAFolderOfThisRepository(t *testing.T) {
	root := setupRepo(t)
	capture(t)
	inside := filepath.Join(root, "docs")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	taskWorkedIn(t, inside)

	if err := start("123", ""); err != nil {
		t.Fatalf("start where the task is worked: %v", err)
	}
	if !branchExists(root, "task/123") {
		t.Error("the branch should exist")
	}
}

// What cannot be compared refuses nothing. A place amenbo could not be asked about, and one
// lying in no repository, each leave nothing to hold the start to — and of the two failures,
// being unable to cut a worktree is the worse — so it is cut, and the reason is on stderr.
func TestStartCutsOnAPlaceItCannotCompare(t *testing.T) {
	for _, tc := range []struct {
		name  string
		stand func(t *testing.T)
	}{
		{"the read fails", func(t *testing.T) { amenboAnswers(t, "", errors.New("no amenbo on the PATH")) }},
		{"the folder is in no repository", func(t *testing.T) { taskWorkedIn(t, t.TempDir()) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := setupRepo(t)
			_, stderr := capture(t)
			tc.stand(t)

			if err := start("123", ""); err != nil {
				t.Fatalf("start: %v", err)
			}
			if !branchExists(root, "task/123") {
				t.Error("the worktree should have been cut anyway")
			}
			if !strings.Contains(stderr.String(), "cutting here") {
				t.Errorf("the check it could not make should be said: %q", stderr)
			}
		})
	}
}

// Only the cut is held to the task's folder. A teardown takes away a worktree that is
// already there, and there is no second repository it could have meant.
func TestFinishIsNotHeldToTheFolderTheTaskNames(t *testing.T) {
	root := setupRepo(t)
	capture(t)
	worktree := startOK(t, "123")
	taskWorkedIn(t, t.TempDir())

	if err := finish("123", "", false); err != nil {
		t.Fatalf("finish: %v", err)
	}
	if _, err := os.Stat(worktree); !os.IsNotExist(err) {
		t.Errorf("%s should be gone, stat err = %v", worktree, err)
	}
	if branchExists(root, "task/123") {
		t.Error("the branch should be gone")
	}
}

func TestStartCutsTheBranchFromTheGivenBase(t *testing.T) {
	root := setupRepo(t)
	capture(t)
	if _, err := git(root, "branch", "release"); err != nil {
		t.Fatal(err)
	}
	if _, err := git(root, "commit", "--allow-empty", "-m", "past release"); err != nil {
		t.Fatal(err)
	}

	if err := start("123", "release"); err != nil {
		t.Fatalf("start: %v", err)
	}
	if !isMerged(root, "task/123", "release") {
		t.Error("the branch should sit on release, not on the branch the repo is on")
	}
}

// Nothing is filled in first, and no name is assumed: what a worktree is cut from is the
// branch the repository is standing on, whatever it is called. A trunk named anything but
// `main` is exactly the repository a fixed default turns away.
func TestStartCutsFromWhateverBranchTheRepositoryIsOn(t *testing.T) {
	root := setupRepo(t)
	capture(t)
	if _, err := git(root, "branch", "-m", "trunk"); err != nil {
		t.Fatal(err)
	}
	if _, err := git(root, "commit", "--allow-empty", "-m", "on trunk"); err != nil {
		t.Fatal(err)
	}

	if err := start("123", ""); err != nil {
		t.Fatalf("start: %v", err)
	}
	if !isMerged(root, "task/123", "trunk") {
		t.Error("the branch should sit on trunk, the branch the repository was on")
	}
}

// The teardown measures against the same answer, so a repository that never says `main`
// is not told its work is unmerged by a branch it does not have.
func TestFinishMeasuresAgainstTheBranchTheRepositoryIsOn(t *testing.T) {
	root := setupRepo(t)
	capture(t)
	if _, err := git(root, "branch", "-m", "trunk"); err != nil {
		t.Fatal(err)
	}
	worktree := startOK(t, "123")
	if _, err := git(worktree, "commit", "--allow-empty", "-m", "work"); err != nil {
		t.Fatal(err)
	}

	if err := finish("123", "", false); err == nil {
		t.Fatal("finish should refuse a branch trunk does not hold")
	}
	if _, err := git(root, "merge", "task/123"); err != nil {
		t.Fatal(err)
	}
	if err := finish("123", "", false); err != nil {
		t.Fatalf("finish after the merge: %v", err)
	}
}

func TestFinishRemovesTheWorktreeTheBranchAndTheEmptiedDir(t *testing.T) {
	root := setupRepo(t)
	capture(t)
	worktree := startOK(t, "123")
	_, worktreeBase, _, err := paths("123")
	if err != nil {
		t.Fatal(err)
	}

	if err := finish("123", "", false); err != nil {
		t.Fatalf("finish: %v", err)
	}
	if _, err := os.Stat(worktree); !os.IsNotExist(err) {
		t.Errorf("%s should be gone, stat err = %v", worktree, err)
	}
	if branchExists(root, "task/123") {
		t.Error("the branch should be gone")
	}
	if _, err := os.Stat(worktreeBase); !os.IsNotExist(err) {
		t.Errorf("the emptied sibling dir should be pruned, stat err = %v", err)
	}
}

// Another task's worktree still living in the sibling dir keeps it: pruning is for a dir
// with nothing left in it.
func TestFinishKeepsTheDirWhileAnotherTaskLivesThere(t *testing.T) {
	setupRepo(t)
	capture(t)
	startOK(t, "123")
	other := startOK(t, "456")
	_, worktreeBase, _, err := paths("123")
	if err != nil {
		t.Fatal(err)
	}

	if err := finish("123", "", false); err != nil {
		t.Fatalf("finish: %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("the other task's worktree should be untouched: %v", err)
	}
	if _, err := os.Stat(worktreeBase); err != nil {
		t.Errorf("the sibling dir should survive: %v", err)
	}
}

func TestFinishRefusesADirtyWorktree(t *testing.T) {
	setupRepo(t)
	capture(t)
	worktree := startOK(t, "123")
	if err := os.WriteFile(filepath.Join(worktree, "wip.txt"), []byte("half a thought"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := finish("123", "", false)
	if err == nil || !strings.Contains(err.Error(), "uncommitted") {
		t.Fatalf("finish = %v — want a refusal naming the uncommitted work", err)
	}
	if _, err := os.Stat(worktree); err != nil {
		t.Errorf("a refused teardown removes nothing: %v", err)
	}
}

func TestFinishRefusesABranchThatIsNotMergedYet(t *testing.T) {
	setupRepo(t)
	capture(t)
	worktree := startOK(t, "123")
	if _, err := git(worktree, "commit", "--allow-empty", "-m", "work"); err != nil {
		t.Fatal(err)
	}

	err := finish("123", "", false)
	if err == nil || !strings.Contains(err.Error(), "task/123") {
		t.Fatalf("finish = %v — want a refusal naming the branch", err)
	}
}

// --force is the one way to discard work on purpose, and it discards both refusals.
func TestFinishForceTearsDownDirtyAndUnmerged(t *testing.T) {
	root := setupRepo(t)
	capture(t)
	worktree := startOK(t, "123")
	if _, err := git(worktree, "commit", "--allow-empty", "-m", "work"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "wip.txt"), []byte("half a thought"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := finish("123", "", true); err != nil {
		t.Fatalf("finish --force: %v", err)
	}
	if _, err := os.Stat(worktree); !os.IsNotExist(err) {
		t.Errorf("%s should be gone, stat err = %v", worktree, err)
	}
	if branchExists(root, "task/123") {
		t.Error("the branch should be gone")
	}
}

func TestFinishSaysSoWhenThereIsNoWorktree(t *testing.T) {
	setupRepo(t)
	capture(t)
	if err := finish("123", "", false); err == nil {
		t.Fatal("want a refusal")
	}
}

// A teardown answers nothing, so the return value stays empty — an eval of it is a
// no-op rather than a stray command.
func TestFinishReturnsNothing(t *testing.T) {
	setupRepo(t)
	stdout, _ := capture(t)
	startOK(t, "123")
	stdout.Reset()

	if err := finish("123", "", false); err != nil {
		t.Fatal(err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q — want nothing", stdout)
	}
}

// Both commands work from a task worktree too: the paths hang off the MAIN worktree
// root, not off wherever one happens to be standing.
func TestTheCommandsResolveTheSamePathsFromInsideAWorktree(t *testing.T) {
	setupRepo(t)
	capture(t)
	worktree := startOK(t, "123")

	chdir(t, worktree)
	_, _, fromInside, err := paths("456")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(filepath.Dir(worktree), "456"); fromInside != want {
		t.Fatalf("paths from inside a worktree = %q, want %q", fromInside, want)
	}
}

// Nothing outside a repository has a worktree to give.
func TestPathsRefuseOutsideAGitRepository(t *testing.T) {
	chdir(t, t.TempDir())
	if _, _, _, err := paths("123"); err == nil {
		t.Fatal("want a refusal")
	}
}

// The quoted line has to be read the same way by a POSIX shell and by PowerShell, so the
// quoting stays inside what both read literally: a space or a backslash is carried through
// untouched, and no escape is spelled that only one of them understands.
func TestShellQuoteSurvivesAnAwkwardPath(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`/tmp/a b/wt`, `'/tmp/a b/wt'`},
		{`C:\Users\a b\repo-worktrees\123`, `'C:\Users\a b\repo-worktrees\123'`},
		{`/tmp/$HOME/wt`, `'/tmp/$HOME/wt'`},
	} {
		got, err := shellQuote(tc.in)
		if err != nil {
			t.Fatalf("shellQuote(%s) = %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("shellQuote(%s) = %s, want %s", tc.in, got, tc.want)
		}
	}
}

// A single quote in the path is the one case the two dialects cannot be served by one line —
// POSIX closes and re-opens the string, PowerShell doubles the quote — so start refuses it
// instead of returning a line that works where it was baked and breaks elsewhere.
func TestShellQuoteRefusesAPathItCannotWriteForBothShells(t *testing.T) {
	if _, err := shellQuote("/tmp/it's here"); err == nil {
		t.Fatal("want a refusal")
	}
}
