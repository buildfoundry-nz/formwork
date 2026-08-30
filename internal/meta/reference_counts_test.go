// reference_counts_test.go — every count site in the operator manual, not the
// one phrasing a substring match happens to see (#328).
//
// WHY THIS EXISTS. docs/reference.md states its registry sizes in THREE places:
// the prose summary near the top ("**26 rule types, 14 preprocessors.**") and
// the bare "N registered." line that opens each of the two sections which
// enumerate them. TestReferenceManualStatesTheRegistryCountsCorrectly next door
// asserts strings.Contains(manual, "26 rule types") — a phrasing that appears
// at exactly one of those three sites. The other two were structurally
// invisible to it, and it showed: 28ddc1f0 registered the 26th type, was forced
// by that test to update the prose line, and left "25 registered." standing at
// the head of the section that documents all 26. The document contradicted
// itself by one across 160 lines, in a file whose own opening section tells the
// reader it "cannot fall behind the engine silently".
//
// WHAT IT ASSERTS, IN TWO ARMS THAT FAIL DIFFERENTLY.
//
//   - POSITIONAL, for the phrasing that names no vocabulary. It locates the
//     "## Rule types" and "## Preprocessors" headings and reads the "N
//     registered." line beneath each. That sentence says nothing about WHAT is
//     registered, so only its position decides which registry it claims about
//     — which is why it is read this way, why a missing or duplicated site is a
//     failure with instructions rather than a silent skip, and why a bare "N
//     registered." that has drifted out of both sections is reported as
//     unattributable rather than checked against a guess.
//
//   - BY THE COUNTED NOUN, for every phrasing that does name one. A number in
//     front of "rule type(s)" or "preprocessor(s)" is a claim about a registry
//     in whatever sentence carries it, so all of them are compared, wherever
//     they sit. This arm is what makes rewording not an escape — and it was
//     added because the positional arm alone was not: see
//     TestCountPhraseSweepFindsARewordedCountSite, which records the plant that
//     went green.
//
// Both compare against len(rules.TypeNames()) and len(preprocess.Names()). The
// prose summary is additionally read as a sentence, because that check is about
// the summary surviving at all rather than about its numbers.
//
// The registries are populated by the blank imports in reference_manual_test.go
// (same package). Those and its minRegisteredTypes floor stay load-bearing: the
// floor is re-asserted here so a dropped import cannot make this comparison a
// tautology of two wrong numbers agreeing.
package meta_test

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/preprocess"
	"github.com/buildfoundry-nz/formwork/internal/rules"
)

// The bare count line that opens an enumerating section, e.g. "26 registered."
// The count line that opens an enumerating section, e.g. "26 registered rule
// types." The registry name is required: a bare "26 registered." names no
// registry, so a reader cannot tell which one it claims about and
// TestReferenceManualStatesTheRegistryCountsCorrectly rejects it. Position
// still decides which section the count belongs to; the name makes the claim
// legible on its own.
var bareRegisteredLine = regexp.MustCompile(`^([0-9]+) registered(?: rule types| preprocessors)?\.`)

// The prose summary near the top of the manual.
var proseCountLine = regexp.MustCompile(
	`Counts at the time of writing: \*\*([0-9]+) rule types, ([0-9]+) preprocessors\.\*\*`)

// registeredCountUnder finds the single "N registered." line belonging to the
// given "## " heading. line is 1-based, for a failure message an editor can act
// on. A non-empty problem means the site could not be read at all, which is
// itself a failure: the guard must not fall silent because the manual was
// reshaped around it.
func registeredCountUnder(lines []string, heading string) (n, line int, problem string) {
	start := -1
	for i, l := range lines {
		if strings.TrimRight(l, " \t") != heading {
			continue
		}
		if start >= 0 {
			return 0, 0, "heading appears more than once, so the count site is ambiguous"
		}
		start = i
	}
	if start < 0 {
		return 0, 0, "heading not found"
	}
	found := -1
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") {
			break
		}
		m := bareRegisteredLine.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		if found >= 0 {
			return 0, 0, `more than one "N registered." line in the section`
		}
		v, err := strconv.Atoi(m[1])
		if err != nil {
			return 0, 0, `unreadable number in the "N registered." line`
		}
		n, found = v, i
	}
	if found < 0 {
		return 0, 0, `no "N registered." line in the section`
	}
	return n, found + 1, ""
}

func TestReferenceManualCountSitesMatchTheLiveRegistries(t *testing.T) {
	manual := referenceManual(t)
	lines := strings.Split(manual, "\n")

	types := len(rules.TypeNames())
	preprocessors := len(preprocess.Names())
	if types < minRegisteredTypes || preprocessors == 0 {
		t.Fatalf("only %d rule types and %d preprocessors registered (want >= %d and > 0) — "+
			"a blank import is missing from reference_manual_test.go and the comparisons "+
			"below would pass against a fraction of the vocabulary",
			types, preprocessors, minRegisteredTypes)
	}

	guarded := map[int]bool{}
	for _, site := range []struct {
		heading string
		what    string
		want    int
	}{
		{"## Rule types", "rule types", types},
		{"## Preprocessors", "preprocessors", preprocessors},
	} {
		got, line, problem := registeredCountUnder(lines, site.heading)
		if problem != "" {
			t.Errorf("docs/reference.md: cannot read the %s count under %q: %s\n"+
				`Keep one "N registered." line under that heading — it is what tells a `+
				"reader how many %s to expect, and it is checked here.",
				site.what, site.heading, problem, site.what)
			continue
		}
		guarded[line] = true
		if got != site.want {
			t.Errorf("docs/reference.md:%d says %q under %q, but %d %s are registered.\n"+
				"Update the line to %q rather than the count a reader is asked to trust.",
				line, strconv.Itoa(got)+" registered.", site.heading,
				site.want, site.what, strconv.Itoa(site.want)+" registered.")
		}
	}

	// Every count that names the vocabulary it counts, wherever it is written.
	// This is the arm that survives a rewording — see
	// TestCountPhraseSweepFindsARewordedCountSite. It subsumes the prose
	// summary's two numbers; the summary is still read separately below,
	// because that check is about the SENTENCE surviving, not the number.
	live := map[string]int{"rule types": types, "preprocessors": preprocessors}
	for _, site := range countPhrases(lines) {
		want := live[site.noun]
		if site.n == want {
			continue
		}
		t.Errorf("docs/reference.md:%d says %q; %d %s are registered.\n"+
			"Every count naming its vocabulary is read here, in whatever sentence "+
			"carries it — rewording a stale number is not a way past this.",
			site.line, site.text, want, site.noun)
	}

	// A count site outside both sections is a site nothing above reads.
	for _, line := range unguardedCountLines(lines, guarded) {
		t.Errorf(`docs/reference.md:%d states a count ("%s") outside the two sections `+
			"this test reads.\nEither move it under its section heading or extend this "+
			"test to cover it; an unguarded count is how docs/reference.md:188 went "+
			"stale in the first place.", line, strings.TrimSpace(lines[line-1]))
	}

	proseTypes, prosePreprocessors, ok := proseCounts(manual)
	if !ok {
		t.Fatalf("docs/reference.md no longer carries a readable prose count summary " +
			"(\"Counts at the time of writing: **N rule types, M preprocessors.**\").\n" +
			"That sentence is the first count a reader meets; keep it and it stays checked.")
	}
	for _, site := range []struct {
		got  int
		what string
		want int
	}{
		{proseTypes, "rule types", types},
		{prosePreprocessors, "preprocessors", preprocessors},
	} {
		if site.got != site.want {
			t.Errorf("docs/reference.md prose summary says %d %s; %d are registered",
				site.got, site.what, site.want)
		}
	}
}

// unguardedCountLines reports the 1-based lines carrying a bare "N registered."
// that no section above claimed. A count nothing reads is the defect this file
// exists for, so finding one is a failure rather than a shrug.
func unguardedCountLines(lines []string, guarded map[int]bool) []int {
	var out []int
	for i, l := range lines {
		if bareRegisteredLine.MatchString(l) && !guarded[i+1] {
			out = append(out, i+1)
		}
	}
	return out
}

// proseCounts reads the manual's prose summary. ok is false when the sentence
// is gone or its numbers cannot be read, which is a failure and not a skip: a
// summary the parser cannot find is a summary nothing checks.
func proseCounts(manual string) (types, preprocessors int, ok bool) {
	m := proseCountLine.FindStringSubmatch(manual)
	if m == nil {
		return 0, 0, false
	}
	types, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, 0, false
	}
	preprocessors, err = strconv.Atoi(m[2])
	if err != nil {
		return 0, 0, false
	}
	return types, preprocessors, true
}

// The readers above are the guard, so their refusals are load-bearing: a
// manual reshaped around them — heading renamed, count line deleted, summary
// sentence dropped — must fail loudly rather than quietly checking nothing.
// That is the same fail-open shape #328 is an instance of, one level up, and
// the real document exercises none of these branches. Synthetic documents do.
func TestCountSiteReadersRefuseADocumentTheyCannotRead(t *testing.T) {
	const heading = "## Rule types"
	for _, tc := range []struct {
		name        string
		doc         []string
		wantN       int
		wantLine    int
		wantProblem string
	}{
		{
			name:     "well formed",
			doc:      []string{heading, "", "26 registered. Parameters below", "", "## Preprocessors"},
			wantN:    26,
			wantLine: 3,
		},
		{
			name:        "heading renamed out from under the reader",
			doc:         []string{"## Rule kinds", "26 registered."},
			wantProblem: "heading not found",
		},
		{
			name:        "heading duplicated, so the site is ambiguous",
			doc:         []string{heading, "26 registered.", "## Other", heading, "26 registered."},
			wantProblem: "more than once",
		},
		{
			name:        "count line deleted",
			doc:         []string{heading, "Parameters below are the strictly-decoded set."},
			wantProblem: `no "N registered." line`,
		},
		{
			name:        "two count lines in one section",
			doc:         []string{heading, "26 registered.", "26 registered."},
			wantProblem: "more than one",
		},
		{
			name:        "a number too large to read",
			doc:         []string{heading, "99999999999999999999999 registered."},
			wantProblem: "unreadable number",
		},
		{
			name:        "the next section's count is not borrowed",
			doc:         []string{heading, "Prose only.", "## Preprocessors", "14 registered."},
			wantProblem: `no "N registered." line`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, line, problem := registeredCountUnder(tc.doc, heading)
			if tc.wantProblem != "" {
				if !strings.Contains(problem, tc.wantProblem) {
					t.Fatalf("want a refusal mentioning %q; got problem=%q n=%d line=%d",
						tc.wantProblem, problem, n, line)
				}
				return
			}
			if problem != "" {
				t.Fatalf("want the count read cleanly; got problem=%q", problem)
			}
			if n != tc.wantN || line != tc.wantLine {
				t.Fatalf("want n=%d at line %d; got n=%d at line %d", tc.wantN, tc.wantLine, n, line)
			}
		})
	}
}

// countSite is one place the manual states a registry size in words a reader
// can act on: the number, the vocabulary it counts, and where to find it.
type countSite struct {
	line int
	n    int
	noun string // "rule types" or "preprocessors", normalised
	text string
}

// countPhrase matches a number in front of one of the two vocabularies this
// manual counts. Singular and plural both, because "1 preprocessor" is the
// spelling a shrinking registry would produce, and case-insensitively because a
// sentence can start with one.
//
// It deliberately does NOT match the bare "N registered." line: that phrasing
// names no vocabulary, so which registry it is claiming about is decided by
// which section it sits under, and that is registeredCountUnder's job.
var countPhrase = regexp.MustCompile(`(?i)\b([0-9]+)[ \t]+(rule types?|preprocessors?)\b`)

// countPhrases finds every "<number> rule type(s)" and "<number>
// preprocessor(s)" in the manual, wherever it sits, in document order.
//
// An unreadable number is skipped rather than reported, and that is safe here
// where it would not be elsewhere: the regex admits only digits, so the sole way
// to reach it is an integer too large for the platform's int — a number no
// registry can have, in a sentence no reader would act on. Every readable count
// is still compared.
func countPhrases(lines []string) []countSite {
	var out []countSite
	for i, l := range lines {
		for _, m := range countPhrase.FindAllStringSubmatch(l, -1) {
			n, err := strconv.Atoi(m[1])
			if err != nil {
				continue
			}
			noun := strings.ToLower(m[2])
			if !strings.HasSuffix(noun, "s") {
				noun += "s"
			}
			out = append(out, countSite{line: i + 1, n: n, noun: noun, text: m[0]})
		}
	}
	return out
}

// TestCountPhraseSweepFindsARewordedCountSite is the recurrence guard for this
// file's own blind spot. registeredCountUnder and unguardedCountLines between
// them hold the three count sites the manual carries TODAY, and both key off
// one literal phrasing — the bare "N registered." line. A fourth site written
// any other way is invisible to both: planting "The engine ships 25 rule types
// today." after the prose summary in the real docs/reference.md left
// `go test ./internal/meta/ -count=1` green, with the manual then contradicting
// its own binary in a sentence no assertion could see. That is #328's shape
// exactly, one turn later — the same document, the same defect, a different
// wording.
//
// So the sweep keys off the NOUN instead of the layout. A number in front of
// "rule types" or "preprocessors" is a claim about a registry no matter which
// sentence carries it, and the gate below compares every one of them.
func TestCountPhraseSweepFindsARewordedCountSite(t *testing.T) {
	doc := []string{
		"Counts at the time of writing: **26 rule types, 14 preprocessors.** If your",
		"binary reports different numbers, believe your binary.",
		"The engine ships 25 rule types today.",
		"## Preprocessors",
		"14 registered. Declared per rule as `preprocess:`.",
		"There is exactly 1 preprocessor worth knowing, and the walk reads 491 files.",
	}
	got := countPhrases(doc)
	want := []countSite{
		{line: 1, n: 26, noun: "rule types"},
		{line: 1, n: 14, noun: "preprocessors"},
		{line: 3, n: 25, noun: "rule types"},
		{line: 6, n: 1, noun: "preprocessors"},
	}
	if len(got) != len(want) {
		t.Fatalf("want %d count phrases (including the reworded one at line 3 and the "+
			"singular at line 6); got %d: %+v", len(want), len(got), got)
	}
	for i, w := range want {
		if got[i].line != w.line || got[i].n != w.n || got[i].noun != w.noun {
			t.Errorf("site %d: got line=%d n=%d noun=%q; want line=%d n=%d noun=%q",
				i, got[i].line, got[i].n, got[i].noun, w.line, w.n, w.noun)
		}
	}
	if none := countPhrases([]string{"The registries are the authority; ask the binary."}); len(none) != 0 {
		t.Errorf("want nothing found in a sentence carrying no count; got %+v", none)
	}
}

func TestUnguardedCountLinesFindsACountNoSectionClaimed(t *testing.T) {
	doc := []string{
		"## Rule types",
		"26 registered.",
		"## Appendix",
		"7 registered.",
	}
	got := unguardedCountLines(doc, map[int]bool{2: true})
	if len(got) != 1 || got[0] != 4 {
		t.Fatalf("want the unclaimed count at line 4 reported; got %v", got)
	}
	if none := unguardedCountLines(doc, map[int]bool{2: true, 4: true}); len(none) != 0 {
		t.Fatalf("want nothing reported once both sites are claimed; got %v", none)
	}
}

func TestProseCountsRefusesASummaryItCannotRead(t *testing.T) {
	const good = "Counts at the time of writing: **26 rule types, 14 preprocessors.** If your"
	types, preprocessors, ok := proseCounts(good)
	if !ok || types != 26 || preprocessors != 14 {
		t.Fatalf("want 26/14 read from the summary; got %d/%d ok=%v", types, preprocessors, ok)
	}
	for _, bad := range []struct {
		name string
		doc  string
	}{
		{"sentence removed", "The registries are the authority; ask the binary."},
		{"reworded past the reader", "Counts today: 26 rule types, 14 preprocessors."},
		{
			"a number too large to read",
			"Counts at the time of writing: **99999999999999999999999 rule types, 14 preprocessors.**",
		},
	} {
		t.Run(bad.name, func(t *testing.T) {
			if _, _, ok := proseCounts(bad.doc); ok {
				t.Fatal("want the summary refused as unreadable; got ok=true, which would " +
					"check nothing while looking green")
			}
		})
	}
}
