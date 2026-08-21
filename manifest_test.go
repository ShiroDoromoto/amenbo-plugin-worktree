package main

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
)

// The manifest as this test asks about it — the fields that have to agree with the code and
// the build beside them, not the whole schema the catalog validates. What no test here can
// see is whether the release it quotes is the newest one; that is the release procedure's.
type manifest struct {
	Name     string           `json:"name"`
	Repo     string           `json:"repo"`
	OS       []string         `json:"os"`
	PayloadV int              `json:"payload_v"`
	Events   []string         `json:"events"`
	Config   []any            `json:"config"`
	Assets   map[string]asset `json:"assets"`
}

type asset struct {
	URL      string `json:"url"`
	Checksum string `json:"checksum"`
}

func readManifest(t *testing.T) manifest {
	t.Helper()
	raw, err := os.ReadFile("dev/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var m manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// platformKeys reads the one list the release bakes from, the Makefile's PLATFORMS, rather
// than keeping a second copy of it here. A platform is added in one place, and what these
// tests then catch is the manifest that did not follow it.
func platformKeys(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile("Makefile")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if rest, found := strings.CutPrefix(line, "PLATFORMS :="); found {
			return strings.Fields(rest)
		}
	}
	t.Fatal("the Makefile no longer declares PLATFORMS — these tests read the platform list from it")
	return nil
}

// entryKey is the asset key a platform is published under. They are the same word except on
// macOS, where one universal build covers every Mac: the catalog names that key for the
// operating system, while the file it points at still says which build it is.
func entryKey(platform string) string {
	return strings.TrimSuffix(platform, "-universal")
}

// The subscription and the code have to name the same event. One the manifest names and the
// hook does not read is a launch with nothing to say; one the hook reads and the manifest does
// not name is a sentence never reached, since Amenbo starts a plugin for what it subscribed to
// and nothing else.
func TestTheManifestSubscribesToTheEventTheHookReads(t *testing.T) {
	events := readManifest(t).Events

	if len(events) != 1 || events[0] != eventStatusChanged {
		t.Errorf("the manifest subscribes to %v, the hook reads %q", events, eventStatusChanged)
	}
}

// `payload_v` is the contract Amenbo writes its document to, and contractVersion is the one the
// plugin reads. A document announcing a version the plugin does not read is dropped where it
// arrives, so declaring one number and reading another leaves the plugin silent on every event
// without failing anywhere.
func TestTheDeclaredPayloadVersionIsTheOneTheCodeReads(t *testing.T) {
	if got := readManifest(t).PayloadV; got != contractVersion {
		t.Errorf("the manifest declares payload_v %d, the code reads %d", got, contractVersion)
	}
}

// This plugin asks the user for nothing, and the code says so twice over: the settings Amenbo
// writes into the input document are read by nobody, and no secret is looked up in the
// environment. A field declared here would draw a form whose answers go nowhere.
func TestTheManifestDeclaresNoSettings(t *testing.T) {
	if config := readManifest(t).Config; len(config) != 0 {
		t.Errorf("the manifest declares %d setting(s), the plugin reads none", len(config))
	}
}

// Every platform the release bakes has to be published under a key, and nothing may be
// published that no run bakes: a key with no build behind it is an install that 404s on the
// machine it was offered to.
func TestEveryPlatformTheBuildBakesIsPublished(t *testing.T) {
	assets := readManifest(t).Assets

	for _, platform := range platformKeys(t) {
		if _, published := assets[entryKey(platform)]; !published {
			t.Errorf("the build bakes %q, the manifest publishes no %q asset", platform, entryKey(platform))
		}
	}
	if len(assets) != len(platformKeys(t)) {
		t.Errorf("the manifest publishes %d asset(s) for %d baked platform(s)", len(assets), len(platformKeys(t)))
	}
}

// `os` is what Amenbo weighs against the machine before it offers the plugin at all, so it
// follows from the same list: the operating systems the baked platforms name, in the order
// they are baked, and no others.
func TestTheDeclaredOSesAreTheOnesTheBuildBakesFor(t *testing.T) {
	var want []string
	for _, platform := range platformKeys(t) {
		name, _, _ := strings.Cut(platform, "-")
		if len(want) == 0 || want[len(want)-1] != name {
			want = append(want, name)
		}
	}

	got := readManifest(t).OS
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("the manifest declares %v, the build bakes for %v", got, want)
	}
}

// One release, quoted the same way by every key. The tag is written twice in each url — once
// in the path and once in the filename — and every asset has to agree with every other, since
// a single line left behind at the previous release serves that one platform an old binary
// whose digest still checks out.
//
// The digest itself is only shaped here. Whether it is the digest of the file it names is for
// the release procedure to settle, against the bytes it downloaded from the release.
func TestEveryAssetQuotesOneRelease(t *testing.T) {
	m := readManifest(t)
	url := regexp.MustCompile(`^https://github\.com/(.+)/releases/download/(v\d+)/worktree-(v\d+)-([a-z0-9-]+)\.tar\.gz$`)
	digest := regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

	tags := map[string]string{}
	for _, platform := range platformKeys(t) {
		key := entryKey(platform)
		published, ok := m.Assets[key]
		if !ok {
			continue // already reported, by the test that asks for the key at all
		}
		parts := url.FindStringSubmatch(published.URL)
		if parts == nil {
			t.Errorf("%s: %q is not a release asset of this repository", key, published.URL)
			continue
		}
		repo, inPath, inName, named := parts[1], parts[2], parts[3], parts[4]
		if repo != m.Repo {
			t.Errorf("%s: the url names %q, the manifest names %q", key, repo, m.Repo)
		}
		if inPath != inName {
			t.Errorf("%s: the url is under %s and the file says %s", key, inPath, inName)
		}
		if named != platform {
			t.Errorf("%s: the url points at the %s build", key, named)
		}
		if !digest.MatchString(published.Checksum) {
			t.Errorf("%s: %q is not a sha256 digest", key, published.Checksum)
		}
		tags[inPath] = key
	}

	if len(tags) > 1 {
		t.Errorf("the assets are spread over %d releases: %v", len(tags), tags)
	}
}
