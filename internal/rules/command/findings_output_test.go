package command

import (
	"strings"
	"testing"
)

// findingsExist is a stand-in for "this path is in the tree under root".
func findingsExist(known ...string) func(string) bool {
	set := map[string]bool{}
	for _, k := range known {
		set[k] = true
	}
	return func(rel string) bool { return set[rel] }
}

// A command rule's finding is one message with no path, so the journal that
// records it cannot say WHICH FILE the rule was about. 71% of all gate fires
// come from command rules and every one of them is pathless, which is why the
// repair pairer can pair almost none of them (TakeoffQS #18214).
//
// Detectors already print paths. What was missing is a contract by which the
// engine reads them, so the structure the detector already had was thrown away
// at the engine boundary. findings-v1 is that contract: the gofmt/vet
// convention every Go tool already speaks.
func TestParseFindings_OnePerPathLine(t *testing.T) {
	out := []byte(strings.Join([]string{
		"docs/architecture/plan-flow.md:19951: stale: regenerate with `derive run`",
		"schema/out/persist-census.json: census is behind its inputs",
		"api-factory/internal/db/pool.go:12: under a frozen prefix with no ledger entry",
	}, "\n"))

	got := parseFindings(out, findingsExist(
		"docs/architecture/plan-flow.md",
		"schema/out/persist-census.json",
		"api-factory/internal/db/pool.go",
	))

	if len(got) != 3 {
		t.Fatalf("got %d finding(s), want 3 — one per path line:\n%+v", len(got), got)
	}
	if got[0].Path != "docs/architecture/plan-flow.md" || got[0].Line != 19951 {
		t.Fatalf("finding 0 = %+v, want the path and its line", got[0])
	}
	if got[0].Message != "stale: regenerate with `derive run`" {
		t.Fatalf("finding 0 message = %q, want the text after the location", got[0].Message)
	}
	// A location with no line number is still a location. Line 0 means "the
	// file", which is what the journal's nullable line column already encodes.
	if got[1].Path != "schema/out/persist-census.json" || got[1].Line != 0 {
		t.Fatalf("finding 1 = %+v, want the path with no line", got[1])
	}
	if got[2].Line != 12 {
		t.Fatalf("finding 2 = %+v, want line 12", got[2])
	}
}

// Everything that is not a location line still has to arrive. A contract that
// silently drops a detector's summary, cure or header would trade one kind of
// blindness for another, so the remainder becomes a single pathless finding.
func TestParseFindings_UnparsedLinesSurviveAsOnePathlessFinding(t *testing.T) {
	out := []byte(strings.Join([]string{
		"[derive] 2 artefact(s) are behind their inputs",
		"schema/out/persist-census.json: census is behind its inputs",
		"Cure: go -C scripts/dev/derive run . run --root .",
	}, "\n"))

	got := parseFindings(out, findingsExist("schema/out/persist-census.json"))

	if len(got) != 2 {
		t.Fatalf("got %d finding(s), want 2 — the located one plus one for the remainder:\n%+v", len(got), got)
	}
	var remainder string
	for _, m := range got {
		if m.Path == "" {
			remainder = m.Message
		}
	}
	if remainder == "" {
		t.Fatal("no pathless finding — the header and the cure were dropped, and a contract that loses the cure is worse than no contract")
	}
	if !strings.Contains(remainder, "2 artefact(s) are behind") {
		t.Fatalf("remainder = %q, want the header line kept", remainder)
	}
	if !strings.Contains(remainder, "Cure: go -C scripts/dev/derive") {
		t.Fatalf("remainder = %q, want the cure kept — it is the highest-trust teaching channel the repo has", remainder)
	}
}

// The discriminator is the TREE, not the punctuation. Plenty of ordinary
// output is "word: prose" — `go: downloading`, `WARNING: ...` — and guessing
// from shape alone would mint findings against paths that do not exist, which
// is a worse failure than the pathless finding it replaced: a wrong path sends
// a reader somewhere real and unrelated.
func TestParseFindings_ProseThatLooksLikeALocationIsNotAPath(t *testing.T) {
	out := []byte(strings.Join([]string{
		"go: downloading gopkg.in/yaml.v3 v3.0.1",
		"WARNING: scopes flag may not work as expected",
		"api-factory/internal/db/pool.go:12: the real one",
	}, "\n"))

	got := parseFindings(out, findingsExist("api-factory/internal/db/pool.go"))

	for _, m := range got {
		if m.Path != "" && m.Path != "api-factory/internal/db/pool.go" {
			t.Fatalf("invented a finding against %q, which is not a path in the tree", m.Path)
		}
	}
	located := 0
	for _, m := range got {
		if m.Path != "" {
			located++
		}
	}
	if located != 1 {
		t.Fatalf("got %d located finding(s), want exactly 1:\n%+v", located, got)
	}
}

// No output is no findings — never an empty pathless one, which would report a
// violation with nothing in it.
func TestParseFindings_EmptyOutputYieldsNothing(t *testing.T) {
	if got := parseFindings(nil, findingsExist()); len(got) != 0 {
		t.Fatalf("got %d finding(s) from no output, want 0:\n%+v", len(got), got)
	}
	if got := parseFindings([]byte("  \n\t\n"), findingsExist()); len(got) != 0 {
		t.Fatalf("got %d finding(s) from blank output, want 0:\n%+v", len(got), got)
	}
}

// A path that escapes the tree is not a location this engine will report.
// Accepting one would let a detector point a finding at anything on the disk.
func TestParseFindings_RefusesEscapingAndAbsolutePaths(t *testing.T) {
	out := []byte(strings.Join([]string{
		"/etc/passwd:1: absolute",
		"../outside/thing.go:2: escaping",
	}, "\n"))

	got := parseFindings(out, func(string) bool { return true })

	for _, m := range got {
		if m.Path != "" {
			t.Fatalf("accepted %q as a finding path — a location must be inside the tree under evaluation", m.Path)
		}
	}
}
