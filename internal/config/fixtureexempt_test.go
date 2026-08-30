// fixtureexempt_test.go — #336, the parse-time half.
//
// `fixture_exempt` reached the engine as a raw assignment: no trim, no
// validation, so any non-empty byte sequence bought the exemption. Three
// spaces flipped `formwork lint` from FAIL/exit 1 to OK/exit 0 and rendered
// `heavy-blank: fixture-exempt (declared):` on the escape-hatch census — a
// disclosure line with nothing disclosed. The gate exists so the gap is a
// DECISION (internal/meta/fixturecoverage.go), and whitespace is not one.
//
// The sibling reasons in this same package are refused at load time on exactly
// this predicate: internal/config/scan.go:94 for scan.ignore, :115 for
// scan.gitignore. This is the field that skipped the house standard.
package config_test

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/command"
)

// heavyExemptRule is the shape the field is actually written on: a heavy rule
// that shells out, carrying no fixtures. decl is spliced in verbatim.
func heavyExemptRule(decl string) string {
	return "rules:\n" +
		"  - id: gofmt-clean\n" +
		"    type: command\n" +
		"    scope: {include: ['**/*.go']}\n" +
		"    params: {cmd: [true]}\n" +
		decl
}

// fastExemptRule is the same declaration on a rule the field does not govern.
// The refusal is deliberately cost-independent: a content-free declaration is
// wrong wherever it is written, and a loader that refused it only on heavy
// rules would leave the fast half of the field parsing whitespace into the
// census.
func fastExemptRule(decl string) string {
	return "rules:\n" +
		"  - id: no-widget\n" +
		"    type: forbidden-pattern\n" +
		"    scope: {include: ['**/*.ts']}\n" +
		"    params: {pattern: WIDGET}\n" +
		decl
}

func TestContentFreeFixtureExemptIsRefusedAtLoad(t *testing.T) {
	cases := []struct {
		name string
		decl string
	}{
		{"three spaces", "    fixture_exempt: \"   \"\n"},
		{"a tab", "    fixture_exempt: \"\t\"\n"},
		{"a newline", "    fixture_exempt: \"\\n\"\n"},
		{"spaces on a fast rule", "    fixture_exempt: \"  \"\n"},
		// The spellings .formwork/rules/fixture-exempt-declares-nothing.yaml
		// names as ITS residue (#336). That header tells a reader the loader
		// holds them wherever the file is loaded, which is only worth reading
		// if something checks it: strings.TrimSpace is Unicode-aware, and these
		// three are exactly what a pattern over the written bytes cannot judge.
		{"a space spelled as a hex escape", "    fixture_exempt: \"\\x20\"\n"},
		{"a space spelled as a unicode escape", "    fixture_exempt: \"\\u0020\"\n"},
		{"a non-breaking space", "    fixture_exempt: \"\u00a0\"\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc, id := heavyExemptRule(tc.decl), "gofmt-clean"
			if strings.Contains(tc.name, "fast") {
				doc, id = fastExemptRule(tc.decl), "no-widget"
			}
			root := writeRepo(t, map[string]string{
				".formwork/formwork.yaml": validRoot,
				".formwork/rules/r.yaml":  doc,
			})
			_, err := config.Load(root)
			if err == nil {
				t.Fatal("loaded a fixture_exempt that declares nothing — the exemption it " +
					"buys is silent, and the census line it prints has nothing after the colon")
			}
			if !strings.Contains(err.Error(), id) {
				t.Fatalf("error %q does not name the rule", err.Error())
			}
			if !strings.Contains(err.Error(), "fixture_exempt") {
				t.Fatalf("error %q does not name the field", err.Error())
			}
		})
	}
}

// TestFixtureExemptIsStoredTrimmed pins the other half of the assignment. The
// reason is rendered verbatim by the escape-hatch census, and its siblings are
// stored trimmed (internal/config/scan.go, both IgnoreEntry and
// GitignoreEntry), so ragged whitespace must not reach the disclosure surface.
func TestFixtureExemptIsStoredTrimmed(t *testing.T) {
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml": validRoot,
		".formwork/rules/r.yaml": heavyExemptRule(
			"    fixture_exempt: \"  drives git state a fixture tree cannot reproduce  \"\n"),
	})
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Rules[0].FixtureExempt; got != "drives git state a fixture tree cannot reproduce" {
		t.Fatalf("FixtureExempt = %q, want it stored trimmed", got)
	}
}

// TestEmptyFixtureExemptIsIndistinguishableFromAbsent holds the line the
// refusal must not cross. `fixture_exempt: ""` and no key at all decode to the
// same Go value, so they cannot be told apart here — and they need not be:
// both are undeclared, which fixture-coverage already reports rather than
// skips. Turning empty into a load error would make a heavy rule's honest
// "I have not decided yet" fatal at parse time instead of reported at lint
// time, which is a different check at a worse altitude.
func TestEmptyFixtureExemptIsIndistinguishableFromAbsent(t *testing.T) {
	for _, decl := range []string{"    fixture_exempt: \"\"\n", ""} {
		root := writeRepo(t, map[string]string{
			".formwork/formwork.yaml": validRoot,
			".formwork/rules/r.yaml":  heavyExemptRule(decl),
		})
		cfg, err := config.Load(root)
		if err != nil {
			t.Fatalf("decl %q: %v", decl, err)
		}
		if got := cfg.Rules[0].FixtureExempt; got != "" {
			t.Fatalf("decl %q: FixtureExempt = %q, want empty", decl, got)
		}
	}
}
