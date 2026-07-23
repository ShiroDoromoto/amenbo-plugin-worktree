package main

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// setupRepo creates a temp git repo with one commit and chdir's into it for the
// duration of the test. The commands derive every path from the current directory, so a
// test has to stand where a caller would.
func setupRepo(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"commit", "--allow-empty", "-m", "init"},
	} {
		if _, err := git(root, args...); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	chdir(t, root)
	return root
}

// chdir moves into dir for the duration of the test and back afterwards.
func chdir(t *testing.T, dir string) {
	t.Helper()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
}

// capture redirects the plugin's two channels into buffers for the duration of the test,
// so a test can assert what went to the return value and what went to the diagnostics —
// the one distinction the whole contract rests on.
func capture(t *testing.T) (stdout, stderr *bytes.Buffer) {
	t.Helper()
	stdout, stderr = &bytes.Buffer{}, &bytes.Buffer{}
	previousOut, previousErr := out, errOut
	out, errOut = stdout, stderr
	t.Cleanup(func() { out, errOut = previousOut, previousErr })
	return stdout, stderr
}

// stdinWith writes doc to a temp file and opens it, standing in for the pipe amenbo
// feeds the plugin.
func stdinWith(t *testing.T, doc string) *os.File {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stdin.json")
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func TestCanonicalIDTakesTheNumberWithOrWithoutAHash(t *testing.T) {
	for _, given := range []string{"123", "#123"} {
		id, err := canonicalID(given)
		if err != nil || id != "123" {
			t.Fatalf("canonicalID(%q) = %q, %v — want 123", given, id, err)
		}
	}
}

// Every other form is refused, because the worktree and branch names are this string:
// two spellings of one task would be two worktrees that never see each other.
func TestCanonicalIDRefusesAnythingButTheNumber(t *testing.T) {
	for _, given := range []string{"", "AMB-T-123", "01H8XYZ", "123a", "12 3"} {
		if id, err := canonicalID(given); err == nil {
			t.Fatalf("canonicalID(%q) = %q — want a refusal", given, id)
		}
	}
}

func TestBaseBranchPrefersTheFlagThenTheSettingThenMain(t *testing.T) {
	configured := input{Config: map[string]any{"base": " develop "}}
	if got := baseBranch("release", configured); got != "release" {
		t.Errorf("the flag should win: got %q", got)
	}
	if got := baseBranch("", configured); got != "develop" {
		t.Errorf("the setting should be used and trimmed: got %q", got)
	}
	if got := baseBranch("", input{}); got != "main" {
		t.Errorf("with neither, main: got %q", got)
	}
}

// amenbo does not judge what a setting means, so a value of another shape is simply not
// a setting this plugin can act on — treated as unset rather than coerced into a branch
// name nothing would match.
func TestSettingIgnoresAValueThatIsNotText(t *testing.T) {
	in := input{Config: map[string]any{"base": 7.0}}
	if got := in.setting("base"); got != "" {
		t.Fatalf("setting = %q — want it ignored", got)
	}
}

func TestReadInputReadsTheDocumentAmenboFeeds(t *testing.T) {
	in := readInput(stdinWith(t, `{"v":1,"event":"task.status_changed","id":123,"new":"in_progress","config":{"base":"dev"}}`))
	if in.V != 1 || in.Event != eventStatusChanged || in.ID != 123 || in.New != statusInProgress {
		t.Fatalf("readInput = %+v", in)
	}
	if got := in.setting("base"); got != "dev" {
		t.Fatalf("setting base = %q", got)
	}
}

// A document that will not parse is reported and dropped: on the hook face nobody is
// waiting for it, and on the command face it carries only optional settings.
func TestReadInputDropsADocumentThatWillNotParse(t *testing.T) {
	_, stderr := capture(t)
	if in := readInput(stdinWith(t, "{ not json")); !reflect.DeepEqual(in, input{}) {
		t.Fatalf("readInput = %+v — want the empty document", in)
	}
	if stderr.Len() == 0 {
		t.Fatal("the dropped document should be reported")
	}
}

func TestReadInputTakesAnEmptyStdinAsNoDocument(t *testing.T) {
	if in := readInput(stdinWith(t, "")); !reflect.DeepEqual(in, input{}) {
		t.Fatalf("readInput = %+v", in)
	}
}

// The id may sit on either side of the flags — Go's flag package stops at the first
// non-flag word, so one Parse would read only the flags that lead.
func TestParseAroundIDReadsFlagsOnEitherSideOfTheID(t *testing.T) {
	for _, args := range [][]string{
		{"123", "--base", "dev"},
		{"--base", "dev", "123"},
	} {
		fs := flag.NewFlagSet("start", flag.ContinueOnError)
		base := fs.String("base", "", "")
		id, extra := parseAroundID(fs, args)
		if id != "123" || *base != "dev" || len(extra) != 0 {
			t.Fatalf("%v → id=%q base=%q extra=%v", args, id, *base, extra)
		}
	}
}

// A second id is a typo, not a batch — the caller is told rather than having one of the
// two started.
func TestParseAroundIDReportsASecondID(t *testing.T) {
	fs := flag.NewFlagSet("start", flag.ContinueOnError)
	id, extra := parseAroundID(fs, []string{"123", "456"})
	if id != "123" || len(extra) != 1 || extra[0] != "456" {
		t.Fatalf("id=%q extra=%v", id, extra)
	}
}
