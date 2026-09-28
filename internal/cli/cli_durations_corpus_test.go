// cli_durations_corpus_test.go — the identity claim, made over THIS
// repository's own corpora rather than a two-rule fixture.
//
// #26's hard constraint is that re-ordering dispatch changes nothing a caller
// can read. The synthetic tests next door pin the mechanism; this one asks the
// question of real rule sets — the repo's own `.formwork/`, the teaching corpus,
// and the five palletra-port corpora, the largest of which carries 704 rules,
// 177 of them `command` escapes. A scheduling change that was going to
// disturb a verdict would have to do it somewhere, and a corpus of four rules
// is not where.
//
// The hint used is the INVERSION of what the run just measured — the slowest
// rule declared fastest and vice versa — so the comparison is not between an
// ordering and itself. Any ordering must produce the same report; the inverted
// one is the ordering furthest from what the engine would otherwise choose.
package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// corpusRoots enumerates the corpora this proof judges: the repository itself
// plus every directory under examples/. It is DERIVED rather than listed,
// because a corpus added later must be judged without anybody remembering to
// add it here — but the count is floored below, so deriving cannot quietly
// judge nothing.
func corpusRoots(t *testing.T) []string {
	t.Helper()
	repo := filepath.Join("..", "..")
	roots := []string{repo}
	entries, err := os.ReadDir(filepath.Join(repo, "examples"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		roots = append(roots, filepath.Join(repo, "examples", e.Name()))
	}
	return roots
}

// splitReport decodes a JSON report into its durations and everything else.
// Durations are separated because they are WALL TIME: two runs of one tree
// differ in them by construction, so comparing them would compare the clock.
// Everything else — findings, suppressed findings, the scan census, the
// summary — is the verdict, and must be byte-identical.
func splitReport(t *testing.T, raw string) (durations map[string]int64, rest string) {
	t.Helper()
	var doc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("report is not JSON: %v\n%s", err, raw)
	}
	if d, ok := doc["durations"]; ok {
		if err := json.Unmarshal(d, &durations); err != nil {
			t.Fatal(err)
		}
		delete(doc, "durations")
	}
	// Re-marshalling a map sorts its keys, so `rest` is a canonical form of
	// everything the run reported except the clock.
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return durations, string(out)
}

// invertedHint writes a durations report whose ranking is the reverse of the
// one measured: the rule that took longest is declared to have taken least.
// The point is to drive the dispatch as far from the engine's own choice as the
// flag permits, not to be a plausible report.
func invertedHint(t *testing.T, durations map[string]int64) string {
	t.Helper()
	var max int64
	for _, ms := range durations {
		if ms > max {
			max = ms
		}
	}
	flipped := make(map[string]int64, len(durations))
	for id, ms := range durations {
		flipped[id] = max - ms
	}
	body, err := json.Marshal(map[string]any{"durations": flipped})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "inverted.json")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestDispatchOrderChangesNoVerdictOnTheRepoCorpora is the acceptance evidence
// for #26's "verdicts, findings and error selection must be byte-identical"
// constraint.
//
// The floors are what stop it passing over nothing. A corpus whose report
// carries no durations exercised no rule the scheduler could have re-ordered,
// and the whole sweep judging fewer than the corpora actually present would be
// a green over a shrunken set — the vacuity shape this repo refuses everywhere
// else.
func TestDispatchOrderChangesNoVerdictOnTheRepoCorpora(t *testing.T) {
	roots := corpusRoots(t)
	if len(roots) < 6 {
		t.Fatalf("only %d corpora found, want at least 6 (this repo plus the examples/ set); "+
			"a sweep over a shrunken set is a pass over nothing", len(roots))
	}
	judged := 0
	for _, root := range roots {
		t.Run(filepath.Base(root), func(t *testing.T) {
			codeA, outA, errA := runCLI(t, "check", "-C", root, "-format", "json")
			if codeA == 2 {
				t.Fatalf("baseline run failed with an engine error, so nothing below is being "+
					"compared\nstderr:\n%s", errA)
			}
			durations, restA := splitReport(t, outA)
			if len(durations) == 0 {
				t.Fatal("the baseline report carries no durations: no rule in this corpus was timed, " +
					"so an ordering sweep over it proves nothing")
			}

			hint := invertedHint(t, durations)
			codeB, outB, errB := runCLI(t, "check", "-C", root, "-format", "json", "--durations", hint)
			_, restB := splitReport(t, outB)

			if codeA != codeB {
				t.Fatalf("exit code changed under an inverted dispatch order: %d -> %d\nstderr:\n%s", codeA, codeB, errB)
			}
			if restA != restB {
				t.Fatalf("the report changed under an inverted dispatch order. Dispatch order decides "+
					"when a rule runs and nothing else (#26).\nwithout the hint:\n%s\nwith it:\n%s", restA, restB)
			}
			if errA != errB {
				t.Fatalf("stderr changed under an inverted dispatch order.\nwithout:\n%s\nwith:\n%s", errA, errB)
			}
			judged++
		})
	}
	if judged == 0 {
		t.Fatal("no corpus was judged")
	}
}
