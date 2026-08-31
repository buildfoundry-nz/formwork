package pattern

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/scan"
	"gopkg.in/yaml.v3"
)

// RED tests for #4 — forbidden-pattern cannot express polarity.
//
// A forbidden-pattern rule matches TOPIC. Whether the text it matched asserts
// that topic or DENIES it is polarity, and no lexical pattern carries polarity.
// RE2 has no lookbehind, so "except when preceded by a denial" cannot be
// written inside params.pattern at all — this is a capability the rule type
// lacks, not a pattern someone wrote badly.
//
// The consequence is a false positive on the one text that states the rule's
// own success condition: prose saying the banned thing did NOT happen. The
// author's only remedy is to delete the words or delete the disclosure, and
// BOTH make the record say less than the version the gate refused. A gate meant
// to keep audit trails accurate ends up taxing accuracy, and the drift is
// invisible because nobody ever sees the sentence that was not written.

// denialSubject is the measured instance: an anti-laundering arm whose pattern
// spans "rather than" exactly as it spans "I just did".
const denialSubject = `\b(rewrote|rewritten|rewriting)\b[^.]{0,60}\b(commits?|message|history)\b`

func denialFile(t *testing.T, body string) *scan.File {
	t.Helper()
	return scan.NewMemFile("subject.md", []byte(body))
}

func denialRule(t *testing.T, deniedBy []string) *forbidden {
	t.Helper()
	c, err := newForbiddenForTest(denialSubject, deniedBy)
	if err != nil {
		t.Fatalf("build rule: %v", err)
	}
	return c
}

// TestDenialSuppressesTheFalsePositive is the measured case from the issue.
func TestDenialSuppressesTheFalsePositive(t *testing.T) {
	t.Parallel()
	const body = "This commits the repair on top rather than rewriting that history.\n"

	if ms, err := denialRule(t, nil).CheckFile(denialFile(t, body)); err != nil || len(ms) != 1 {
		t.Fatalf("without denied_by the arm must still fire (got %d, err %v)", len(ms), err)
	}
	ms, err := denialRule(t, []string{"rather than", "instead of", "not", "never", "without"}).
		CheckFile(denialFile(t, body))
	if err != nil {
		t.Fatalf("CheckFile: %v", err)
	}
	if len(ms) != 0 {
		t.Fatalf("a denial was still reported as a violation: %+v", ms)
	}
}

// TestDenialOnlyEverSuppresses is the safety property. Anything undecidable
// must reach today's verdict, so the second stage can never make a rule weaker
// than it is now — which is what makes it shippable without a long calibration.
func TestDenialOnlyEverSuppresses(t *testing.T) {
	t.Parallel()
	for name, body := range map[string]string{
		"plain assertion":       "I rewrote the commit history.\n",
		"marker after match":    "I rewrote that history, not the tests.\n",
		"marker on prior line":  "never again.\nI rewrote the commit history.\n",
		"punctuation ends it":   "we never ship that. rewriting commit history now.\n",
		"marker far from match": "never mind the seventeen other things I did before I rewrote the history.\n",
	} {
		ms, err := denialRule(t, []string{"rather than", "instead of", "not", "never", "without"}).
			CheckFile(denialFile(t, body))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(ms) != 1 {
			t.Errorf("%s: suppressed a real finding (got %d) — the second stage may only ever SUPPRESS "+
				"a denial it can locate, never widen what counts as one", name, len(ms))
		}
	}
}

// TestDenialMarkerIsNotManufacturedByASlice is the trap the issue names. A
// fixed-size prefix tail has to cut somewhere, and a cut landing inside a word
// can MANUFACTURE a marker — "cannot" sliced to its last three letters is
// "not" — which would suppress a real finding.
func TestDenialMarkerIsNotManufacturedByASlice(t *testing.T) {
	t.Parallel()
	const body = "You cannot rewrite the commit history here.\n"
	// The subject needs the -ing/-ed form, so use a body that genuinely matches.
	const matching = "You cannot claim we rewrote the commit history.\n"
	for _, b := range []string{body, matching} {
		ms, err := denialRule(t, []string{"not", "never"}).CheckFile(denialFile(t, b))
		if err != nil {
			t.Fatalf("CheckFile: %v", err)
		}
		if strings.Contains(b, "rewrote") && len(ms) != 1 {
			t.Fatalf("'cannot' was sliced into the marker 'not' and suppressed a real finding")
		}
	}
}

// TestDenialAllowsOneShortWord keeps the window honest in the other direction:
// a denial with a single word between marker and match still reads as a denial.
func TestDenialAllowsOneShortWord(t *testing.T) {
	t.Parallel()
	const body = "The repair lands on top, not by rewriting that history.\n"
	ms, err := denialRule(t, []string{"not"}).CheckFile(denialFile(t, body))
	if err != nil {
		t.Fatalf("CheckFile: %v", err)
	}
	if len(ms) != 0 {
		t.Fatalf("a denial one short word from the match was reported: %+v", ms)
	}
}

// newForbiddenForTest builds the checker through the real params path, so the
// tests exercise decoding and compilation rather than a hand-built struct.
func newForbiddenForTest(pattern string, deniedBy []string) (*forbidden, error) {
	y := "pattern: '" + strings.ReplaceAll(pattern, "'", "''") + "'\n"
	if len(deniedBy) > 0 {
		y += "denied_by:\n"
		for _, d := range deniedBy {
			y += "  - '" + d + "'\n"
		}
	}
	var n yaml.Node
	if err := yaml.Unmarshal([]byte(y), &n); err != nil {
		return nil, err
	}
	c, err := newForbidden(n.Content[0])
	if err != nil {
		return nil, err
	}
	return c.(*forbidden), nil
}
