// Command worktree is amenbo's official worktree plugin: it gives each task its own
// git worktree, and takes it away again. It is also the reference implementation of
// the amenbo plugin contract, so it deliberately exercises both faces a plugin has.
//
//   - The observation face. amenbo fires the plugin with NO arguments and the event's
//     JSON on stdin. Nobody is waiting for it, so it only ever ADVISES: it writes a
//     suggestion to stderr and touches nothing. A hook that made worktrees by itself
//     would be a side effect nobody asked for, on a write path that cannot veto it.
//   - The command face. A person or an AI invokes it on purpose
//     (`amenbo plugin run worktree start 123`) and waits for an answer. Arguments
//     arrive on argv; stdout is the machine return value amenbo relays back verbatim;
//     stderr is the human diagnostics; the exit code is the verdict.
//
// A task's worktree lives OUTSIDE the repo, in a sibling dir:
//
//	<repo>/../<repo-name>-worktrees/<id>/   git worktree checkout on task/<id>
//
// Outside-the-repo is deliberate: the checkout is a pure development environment. It
// has no bound amenbo folder in its ancestry, so amenbo commands run there cannot
// reach the real backlog and cannot be confused for backlog moves.
//
// What this plugin writes is git, and only git. It does read a task back from amenbo, to
// see which folder that task is worked in, but reserving one, commenting on it and closing
// it are amenbo's, run from the main repo — which is also where these commands must be
// invoked, since that is where amenbo finds the project the plugin is enabled for.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

// contractVersion is the payload contract this plugin reads. amenbo leads every
// document it writes with `v` and raises it only on a breaking change — new fields are
// added silently — so a document announcing a different version is one this plugin must
// not guess at.
const contractVersion = 1

// out and errOut are the plugin's two channels, indirected so the tests can read what
// was written to each. The split IS the contract: whatever a caller consumes goes to
// out, whatever a human reads goes to errOut, and mixing them corrupts the return value.
var (
	out    io.Writer = os.Stdout
	errOut io.Writer = os.Stderr
)

// logf writes one diagnostic line to stderr, keeping stdout reserved for the return
// value.
func logf(format string, a ...any) {
	fmt.Fprintf(errOut, format+"\n", a...)
}

// input is the JSON document amenbo writes to the plugin's stdin. Both faces receive
// one, and they overlap: `v` and `config` are always there, while the event fields are
// filled in only when an event fired. Unknown keys are ignored — the contract grows by
// addition, so a plugin that refused them would break on the next amenbo.
type input struct {
	// V is the contract version the document is written to.
	V int `json:"v"`
	// Event is the event's namespace name, e.g. "task.status_changed". Empty on the
	// command face, where nothing fired.
	Event string `json:"event"`
	// ID is the affected record's conversational number — the id a person knows it by.
	ID int64 `json:"id"`
	// New is the record's state after the change, for the events whose name does not
	// already say it.
	New string `json:"new"`
	// Config holds the plugin's own non-secret settings, as the user filled them in.
	// Secrets never appear here: amenbo puts those in the environment instead. This
	// plugin declares none, so what arrives is empty — the field is here because the
	// document always carries it, and a reference implementation should show the shape
	// amenbo writes rather than only the part it happens to read.
	Config map[string]any `json:"config"`
}

// readInput reads the document amenbo feeds on stdin.
//
// amenbo always writes one and closes the pipe, so the read finishes promptly. A hand
// run from a terminal is fed nothing at all, and waiting for a person to type JSON
// would hang the plugin on `worktree help` — so an interactive stdin is skipped rather
// than read. A document that will not parse is reported and dropped: on the hook face
// nobody is waiting for it, and on the command face the settings it carries are
// optional, so neither is worth refusing to run over.
func readInput(f *os.File) input {
	if info, err := f.Stat(); err == nil && info.Mode()&os.ModeCharDevice != 0 {
		return input{}
	}
	raw, err := io.ReadAll(f)
	if err != nil || len(bytes.TrimSpace(raw)) == 0 {
		return input{}
	}
	var in input
	if err := json.Unmarshal(raw, &in); err != nil {
		logf("worktree: ignoring an input document that will not parse: %v", err)
		return input{}
	}
	return in
}

func main() {
	in := readInput(os.Stdin)
	args := os.Args[1:]

	// No arguments is the observation face — amenbo fired us for an event and is not
	// waiting. A word means someone invoked us on purpose.
	if len(args) == 0 {
		hook(in)
		return
	}

	switch args[0] {
	case "start":
		fs := flag.NewFlagSet("start", flag.ExitOnError)
		base := fs.String("base", "", "branch to cut the worktree from (default: the branch the repository is on)")
		id := readID(fs, "start", args[1:])
		do(start(id, *base))
	case "finish":
		fs := flag.NewFlagSet("finish", flag.ExitOnError)
		base := fs.String("base", "", "branch the task's branch must be merged into (default: the branch the repository is on)")
		force := fs.Bool("force", false, "tear down even if the worktree is dirty or the branch is unmerged")
		id := readID(fs, "finish", args[1:])
		do(finish(id, *base, *force))
	case "help", "-h", "--help":
		usage()
	default:
		logf("worktree: unknown command %q", args[0])
		usage()
		os.Exit(2)
	}
}

// do ends the command face on the verdict the exit code carries: 0 for a run that
// produced its return value, 1 for one that failed and has none.
func do(err error) {
	if err != nil {
		logf("worktree: %v", err)
		os.Exit(1)
	}
}

// readID parses one invocation and returns the task id in canonical form, refusing
// anything malformed with exit code 2 — a usage error, not a failed run.
func readID(fs *flag.FlagSet, command string, args []string) string {
	id, extra := parseAroundID(fs, args)
	// The worktree and the branch are named after the id, so a second one is a typo
	// rather than a batch: starting whichever came first would be the wrong task.
	if len(extra) > 0 {
		logf("worktree: %s takes one task id, got extra argument(s): %s", command, strings.Join(extra, " "))
		usage()
		os.Exit(2)
	}
	canonical, err := canonicalID(id)
	if err != nil {
		logf("worktree: %v", err)
		usage()
		os.Exit(2)
	}
	return canonical
}

// parseAroundID parses fs over args in which the task id may sit on either side of the
// flags, and returns the id it found ("" if there is none) plus any further positionals.
//
// Go's flag package stops at the first non-flag word, so a single Parse reads only the
// flags that lead: in `start 123 --base dev` the flag goes unread. Peeling the leading
// word off as the id first has the mirror failure — with a flag in front there is
// nothing to peel and the id is swallowed as a leftover nobody reads. Looping takes
// both, which is what every neighbouring tool does.
func parseAroundID(fs *flag.FlagSet, args []string) (id string, extra []string) {
	for {
		_ = fs.Parse(args)
		if fs.NArg() == 0 {
			return id, extra
		}
		if id == "" {
			id = fs.Arg(0)
		} else {
			extra = append(extra, fs.Arg(0))
		}
		args = fs.Args()[1:]
	}
}

// canonicalID normalizes a task reference to its conversational number, stripping an
// optional leading '#'. Any other form is rejected, and that rejection is the point: the
// worktree dir and the branch are named verbatim from this string, so two invocations
// naming the same task in two different forms would produce two differently-named
// worktrees and slip past the "already exists" refusal — silently double-starting one
// task in two sessions.
func canonicalID(id string) (string, error) {
	id = strings.TrimPrefix(id, "#")
	if id == "" {
		return "", fmt.Errorf("missing <id>")
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return "", fmt.Errorf("task ref %q must be the conversational number (digits only, e.g. 123 or #123), so the worktree and branch names stay canonical and a double start is caught", id)
		}
	}
	return id, nil
}

func usage() {
	logf(`worktree — amenbo's official plugin: one git worktree per task

Usage (through amenbo, from the repository the task belongs to):
  eval "$(amenbo plugin run worktree start <id> [--base <branch>])"
  iex (amenbo plugin run worktree start <id> [--base <branch>])      # PowerShell
  amenbo plugin run worktree finish <id> [--base <branch>] [--force]

start   add a worktree for task <id> on a fresh branch task/<id>, in a sibling dir
        outside the repo, and return the 'cd' into it on stdout.
finish  tear that worktree and its branch down. Refused unless the worktree is clean
        and the branch is merged into <branch>; --force overrides both.

Without --base, both cut from and measure against the branch the repository is
standing on. Name one with --base to take the work somewhere else.

With no arguments the plugin is an observation hook: it reads the fired event on stdin
and only writes a suggestion to stderr. It never creates or removes anything on its own.

The backlog is amenbo's: reserve with 'amenbo task status <id> in_progress', close with
'amenbo task done <id>'. This plugin reads a task to see where it is worked, and writes
nothing but git.`)
}
