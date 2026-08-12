package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// amenboCmd is the binary a plugin reads records back with. There is no second protocol
// and no library to link — a plugin is any executable, so the one route that works in
// every language is amenbo itself, on the PATH the user already has it on.
const amenboCmd = "amenbo"

// runAmenbo runs one amenbo command and returns its stdout. Indirected so a test can
// stand in for the binary rather than read the store of whoever is running the tests.
//
// The environment is inherited untouched, and that is the whole of the read-back path:
// amenbo hands a plugin the store to open and the window to read it through when it
// launches it, because a plugin can work out neither from where it stands — its working
// directory is whatever its launcher happened to be in, and no binding of its own sits
// beneath it.
var runAmenbo = func(args ...string) ([]byte, error) {
	cmd := exec.Command(amenboCmd, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		// Whatever it said, on one line. A refusal to a `--json` read is itself a formatted
		// document, and everything here ends up inside a single line of diagnostics.
		said := strings.Join(strings.Fields(stderr.String()), " ")
		if said == "" {
			said = err.Error()
		}
		return nil, fmt.Errorf("%s %s: %s", amenboCmd, strings.Join(args, " "), said)
	}
	return stdout.Bytes(), nil
}

// taskFolder reads back the folder task id is worked in — one of the folders its project
// is bound to, which is amenbo's answer to a project whose work is spread over several
// repositories. A task states one or it states nothing, and most state nothing, so the
// empty answer is the ordinary case rather than a failure.
//
// The facet is declared because every operation that uses one requires it and never
// defaults it. It settles nothing else here: the window this read goes through is the one
// amenbo handed over in the environment, so `ai` is declared, being the narrower of the two.
func taskFolder(id string) (string, error) {
	raw, err := runAmenbo("task", "show", id, "--json", "--actor", "ai")
	if err != nil {
		return "", err
	}
	// A task naming no folder answers with a null, which leaves the nested struct at its
	// zero value — the same empty string a task amenbo has never heard of would give, and
	// both mean there is nothing here to hold anyone to.
	var record struct {
		At struct {
			Dir string `json:"dir"`
		} `json:"at"`
	}
	if err := json.Unmarshal(raw, &record); err != nil {
		return "", fmt.Errorf("reading back task %s: %w", id, err)
	}
	return record.At.Dir, nil
}
