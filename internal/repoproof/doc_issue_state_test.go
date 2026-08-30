// doc_issue_state_test.go — a published document states what the tool DOES.
// It never carries the issue tracker's state.
//
// WHY THIS EXISTS. Two published documents rotted the same way inside two
// weeks, in opposite directions, and nothing in the repository could see it.
//
//   - SECURITY.md §3 listed, under "Two limits, stated exactly, because they
//     bound what that check is worth", that `formwork hooks install` "still
//     takes over hook wiring that already exists … install does not yet decline
//     to create it (#146)". #146 closed COMPLETED on 2026-08-12 and the
//     install-side refusal landed the next day in 2f8c5bde. The security
//     document went on advertising a fail-open that no longer existed, in the
//     very section that decides what counts as a reportable finding — so a
//     researcher who found the refusal REGRESSING would read SECURITY.md,
//     classify it as an acknowledged limit, and not report it.
//   - docs/quickstart.md §8 told adopters that under the FORMWORK_GIT_ENV
//     hatch, `hooks install` "accepts that root under the hatch and reports
//     success over a gate that cannot run (#179, open)". #179 closed COMPLETED
//     on 2026-08-24; install exits 2 there and writes nothing.
//
// Both sentences were true when written. Neither was true when read. And the
// two documents contradicted each other in the same tree — quickstart's next
// paragraph already said install refuses — because each was written against the
// tracker's state on a different day.
//
// THE CURE IS NOT "REMEMBER TO UPDATE THE DOCS". That is what failed, twice, in
// a repository whose whole argument is that reading does not ratchet. The cure
// is a rule about what a published document is allowed to say: describe the
// behaviour, and let the tracker carry the tracker's state. A published claim
// about behaviour is checkable against the binary — this repository has
// internal/meta and internal/repoproof full of gates that do exactly that. A
// published claim about an ISSUE's state is checkable against nothing in the
// tree, so it is unguardable by construction and rots silently.
//
// Citing an issue number stays legal, and deliberately so: "Issue #167." and
// "#146 D4" are provenance, they age into history rather than into falsehood,
// and stripping them would cost the reader the trail. What is banned is the
// STATE claim wrapped around the number — "(#179, open)", "does not yet …
// (#146)" — because that is the half that has an expiry date.
//
// THIS IS A TEST, NOT A FORMWORK RULE, deliberately, and the reason is scope
// rather than pattern. The published set is derived here from git — the tracked
// Markdown at the repository root, the tracked Markdown directly under docs/,
// and the public overlays — so a document renamed or added is covered the day
// it lands. A forbidden-pattern rule would carry that set as a literal glob
// list, and a glob list that stops matching PASSES: an empty scope is a pass at
// check time, flagged only by `formwork lint`'s empty-scope-rot. This gate
// instead asserts its own scope floor (TestPublishedDocSetCoversWhatRotted) and
// fails when the set it judges shrinks. The detector's own refusals are
// exercised below rather than assumed, for the same reason: a guard nobody
// pointed at a violation is a guard nobody has seen fire.
//
// WHAT IS OUT OF SCOPE, AND WHY. Dated records — docs/plans/, docs/specs/,
// docs/notes/, docs/parity/ — are excluded. Each opens with the date it was
// written and states the world as it was then; "not yet built (#N)" in a
// 2026-07-09 design spec is an accurate record of 2026-07-09, and rewriting it
// to match HEAD would destroy the thing it exists to preserve. Nobody adopts
// formwork by reading a plan. The line is between a document that describes the
// tool to someone using it and a document that records a decision.
package repoproof_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// publishedDocs asks git for the adopter-facing documents: tracked Markdown at
// the repository root, tracked Markdown directly under docs/ (the subdirectories
// are dated records — see the file comment), NOTICE, and the public overlays.
//
// Asking git rather than walking the filesystem keeps an untracked scratch file
// out of the judgement and keeps a deleted-but-present file out of it too.
func publishedDocs(t *testing.T) []string {
	t.Helper()
	needBinary(t, "git")
	cmd := exec.Command("git", "ls-files", "-z", "--",
		"*.md", "docs/*.md", "NOTICE", "tools/publication/public-*.md")
	cmd.Dir = repoRoot(t)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("cannot ask git for the published documents: %v", err)
	}
	var docs []string
	for _, p := range strings.Split(string(out), "\x00") {
		if p == "" {
			continue
		}
		if !isPublishedDocPath(p) {
			continue
		}
		docs = append(docs, p)
	}
	sort.Strings(docs)
	return docs
}

// isPublishedDocPath is the depth filter, held apart from the git call so both
// of its edges can be put under a test rather than inferred from a green run.
// `git ls-files -- *.md` matches at any depth, so this is what keeps the
// judgement to root files, docs/ one level down, and the public overlays.
func isPublishedDocPath(p string) bool {
	switch filepath.ToSlash(filepath.Dir(p)) {
	case ".", "docs", "tools/publication":
		return true
	}
	return false
}

// issueCitation matches a bare issue reference. The trailing boundary keeps
// "#146" from also matching inside "#1465".
var issueCitation = regexp.MustCompile(`#[0-9]+\b`)

// citedAsOpen matches an issue number annotated with its tracker state:
// "(#179, open)", "#179 (still open)", "#179 — unresolved", "#146 is still
// open". Only the forward direction, number then annotation, is matched:
// "fail-open" carries the word "open" at a word boundary all over this
// repository, and a backwards match would read "the fail-open class (#150)" as
// a state claim.
//
// THE COPULA IS PART OF THE ANNOTATION, and leaving it out was this detector's
// own fail-open — see the copula test below, which was found by planting
// `Issue #146 is still open.` in SECURITY.md and watching the suite stay green.
// It is a closed vocabulary rather than "any word": allowing arbitrary text
// between the number and the state word would read "#268 and the open-source
// cut" as a state claim, and there is no phrase-list fallback available here
// because "still open" and "is open" cannot go in outstandingPhrases without
// firing on "the vocabulary is still growing".
var citedAsOpen = regexp.MustCompile(
	`#[0-9]+[ \t]*[,;:—-]?[ \t]*[(\[]?[ \t]*` +
		`(?i:(?:is|was|are|were|remains|stays)[ \t]+)?` +
		`(?i:still[ \t]+)?(?i:open|unfixed|outstanding|unresolved|pending)\b`)

// outstandingPhrases are the ways a document says a defect is live. They are
// spelled out rather than reduced to "still" or "open" because those words do
// honest work in these documents — "the vocabulary is still growing", "a corpus
// that is still on disk", "the fail-open defect class" — and a gate that fires
// on them teaches authors to write around it, which is a weakening.
var outstandingPhrases = []string{
	"not yet",
	"still does not",
	"still cannot",
	"still takes over",
	"still fails to",
	"remains open",
	"remains outstanding",
	"is not fixed",
	"has not been fixed",
	"no fix yet",
	"known gap",
	"known bug",
	"to be fixed",
	"will be fixed",
}

// outstandingLimitation matches any of the phrases above, tolerating the line
// break prose wrapping puts inside them — SECURITY.md wrapped "does not yet"
// across a line at one point in its history, and a phrase a hard wrap can hide
// from the gate is a phrase the gate does not hold.
var outstandingLimitation = regexp.MustCompile(`(?i)\b(?:` + func() string {
	alts := make([]string, len(outstandingPhrases))
	for i, p := range outstandingPhrases {
		alts[i] = strings.ReplaceAll(regexp.QuoteMeta(p), " ", `\s+`)
	}
	return strings.Join(alts, "|")
}() + `)\b`)

// claimWindow is how close an outstanding-limitation phrase has to sit to an
// issue citation, in bytes, before the two read as one claim. It is a window
// rather than "anywhere in the paragraph" so that a long paragraph mentioning
// an issue at one end and a limitation at the other is not accused of joining
// them.
const claimWindow = 240

// staleClaim is one refusal: where it is, which detector fired, and the text.
type staleClaim struct {
	line int
	kind string
	text string
}

// block is one blank-line-delimited run of lines, with its byte offset in the
// document. Claims are matched inside a block because a blank line is where a
// paragraph — and therefore a sentence — ends.
type block struct {
	offset int
	text   string
}

func blocks(doc string) []block {
	var out []block
	var b strings.Builder
	off, start := 0, -1
	flush := func() {
		if start >= 0 && strings.TrimSpace(b.String()) != "" {
			out = append(out, block{offset: start, text: b.String()})
		}
		start = -1
		b.Reset()
	}
	for _, line := range strings.SplitAfter(doc, "\n") {
		if strings.TrimSpace(line) == "" {
			flush()
		} else {
			if start < 0 {
				start = off
			}
			b.WriteString(line)
		}
		off += len(line)
	}
	flush()
	return out
}

func lineOf(doc string, offset int) int {
	if offset > len(doc) {
		offset = len(doc)
	}
	return 1 + strings.Count(doc[:offset], "\n")
}

// staleIssueClaims returns every place the document asserts the tracker's state
// instead of the tool's behaviour.
func staleIssueClaims(doc string) []staleClaim {
	var found []staleClaim
	for _, blk := range blocks(doc) {
		for _, m := range citedAsOpen.FindAllStringIndex(blk.text, -1) {
			found = append(found, staleClaim{
				line: lineOf(doc, blk.offset+m[0]),
				kind: "an issue cited with its tracker state",
				text: strings.Join(strings.Fields(blk.text[m[0]:m[1]]), " "),
			})
		}
		cites := issueCitation.FindAllStringIndex(blk.text, -1)
		if len(cites) == 0 {
			continue
		}
		for _, m := range outstandingLimitation.FindAllStringIndex(blk.text, -1) {
			near := ""
			for _, c := range cites {
				if gap := c[0] - m[1]; gap >= 0 && gap <= claimWindow {
					near = blk.text[c[0]:c[1]]
					break
				}
				if gap := m[0] - c[1]; gap >= 0 && gap <= claimWindow {
					near = blk.text[c[0]:c[1]]
					break
				}
			}
			if near == "" {
				continue
			}
			found = append(found, staleClaim{
				line: lineOf(doc, blk.offset+m[0]),
				kind: "a limitation pinned to " + near,
				text: strings.Join(strings.Fields(blk.text[m[0]:m[1]]), " "),
			})
		}
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].line < found[j].line })
	return found
}

const stateClaimCure = "" +
	"A published document describes what the tool DOES; the tracker carries the tracker's state.\n" +
	"A behaviour claim can be checked against the binary and this repository checks plenty of them.\n" +
	"An issue-state claim can be checked against nothing in the tree, so it rots silently — which is\n" +
	"exactly what SECURITY.md did with #146 and docs/quickstart.md did with #179.\n" +
	"Rewrite the sentence to state HEAD's behaviour. Citing the issue number plainly for provenance\n" +
	"stays legal: \"Issue #167.\" and \"#146 D4\" are fine, \"(#167, open)\" and \"not yet … (#146)\" are not."

// TestPublishedDocsDoNotCarryTrackerState is the gate. It reads the real tree.
func TestPublishedDocsDoNotCarryTrackerState(t *testing.T) {
	root := repoRoot(t)
	docs := publishedDocs(t)
	if len(docs) == 0 {
		t.Fatal("no published documents resolved — this gate would pass over nothing")
	}
	var report []string
	for _, rel := range docs {
		body, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("cannot read %s, so this gate cannot answer for it: %v", rel, err)
		}
		for _, c := range staleIssueClaims(string(body)) {
			report = append(report, "  "+rel+":"+strconv.Itoa(c.line)+"  "+c.kind+" — "+strconv.Quote(c.text))
		}
	}
	if len(report) > 0 {
		t.Fatalf("%d published claim(s) about an issue's state rather than the tool's behaviour:\n%s\n\n%s",
			len(report), strings.Join(report, "\n"), stateClaimCure)
	}
}

// TestPublishedDocSetCoversWhatRotted is the non-vacuity floor. A gate whose
// scope has silently emptied reports the same green as a clean tree.
func TestPublishedDocSetCoversWhatRotted(t *testing.T) {
	got := map[string]bool{}
	for _, d := range publishedDocs(t) {
		got[d] = true
	}
	var missing []string
	floor := publishedDocFloor
	if isDevelopmentTree(t) {
		floor = append(append([]string(nil), floor...), publishedDocFloorDev...)
	}
	for _, want := range floor {
		if !got[want] {
			missing = append(missing, want)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("the published-document set no longer covers %d document(s) this gate is meant to judge:\n  %s\n"+
			"If one was renamed, follow it in publishedDocFloor. If one was deleted, say so there.",
			len(missing), strings.Join(missing, "\n  "))
	}
}

// TestPublishedDocSetStopsAtTheDatedRecords pins the other half of the scope
// decision, and pins it non-vacuously. The exclusion of docs/plans, docs/specs,
// docs/notes and docs/parity is a judgement — those documents open with the date
// they were written and state the world as it was then, so "not yet built (#N)"
// in a 2026-07-09 design spec is an accurate record rather than a rotted claim.
// A judgement nothing tests is a judgement that quietly becomes a bug the first
// time the derivation is edited: widen the glob and this gate starts demanding
// that history be rewritten; narrow it and the adopter documents fall out.
//
// The assertion would pass over a tree with no dated records at all, which is
// why the count of them is checked first.
func TestPublishedDocSetStopsAtTheDatedRecords(t *testing.T) {
	needBinary(t, "git")
	cmd := exec.Command("git", "ls-files", "-z", "--", "docs/*/*.md")
	cmd.Dir = repoRoot(t)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("cannot ask git for the dated records: %v", err)
	}
	var dated []string
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" {
			dated = append(dated, p)
		}
	}
	if len(dated) == 0 {
		t.Fatal("no document below docs/ resolved, so this exclusion is untested — " +
			"if the dated records have moved, follow them here")
	}
	published := map[string]bool{}
	for _, d := range publishedDocs(t) {
		published[d] = true
	}
	var leaked []string
	for _, d := range dated {
		if published[d] {
			leaked = append(leaked, d)
		}
	}
	if len(leaked) > 0 {
		t.Fatalf("%d dated record(s) reached the published-document set:\n  %s\n"+
			"A plan, spec or note states the world on the day it was written. Judging it "+
			"against HEAD asks for history to be rewritten.",
			len(leaked), strings.Join(leaked, "\n  "))
	}
}

// --- the detector's own refusals, exercised rather than assumed --------------

// TestStaleClaimDetectorFiresOnTheTwoSentencesThatRotted feeds the detector the
// exact text SECURITY.md and docs/quickstart.md carried, including the hard wrap
// that put "does not yet" and "(#146)" on different lines.
func TestStaleClaimDetectorFiresOnTheTwoSentencesThatRotted(t *testing.T) {
	cases := []struct {
		name string
		doc  string
		line int
	}{
		{
			name: "SECURITY.md's #146 bullet, wrapped as it shipped",
			doc: "Two limits, stated exactly:\n\n" +
				"- **`formwork hooks install` still takes over hook wiring that already exists**\n" +
				"  — a `core.hooksPath` pointing elsewhere, or hooks in `.git/hooks` — rather than\n" +
				"  refusing. Verify now reports that state; install does not yet decline to create\n" +
				"  it (#146).\n",
			line: 3,
		},
		{
			name: "docs/quickstart.md's (#179, open)",
			doc: "accepts that root under the hatch and reports success over a gate that cannot run\n" +
				"(#179, open). Issue #167.)\n",
			line: 2,
		},
		{
			name: "the wrap that hides the phrase from a line-at-a-time reader",
			doc:  "the shim is not\nyet written for this lane (#12).\n",
			line: 1,
		},
		{
			name: "the annotation on its own, no limitation phrase",
			doc:  "The include-path refusal is #173 (still open).\n",
			line: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := staleIssueClaims(tc.doc)
			if len(got) == 0 {
				t.Fatalf("the detector passed a sentence that rotted in production:\n%s", tc.doc)
			}
			if got[0].line != tc.line {
				t.Errorf("first refusal at line %d, want %d (%+v)", got[0].line, tc.line, got)
			}
		})
	}
}

// TestStaleClaimDetectorFiresOnACopulaBetweenTheNumberAndItsState closes a hole
// this detector shipped with, found by planting `Issue #146 is still open.` in
// SECURITY.md — the sentence #290 is about, written the way an author would
// most naturally write it — and watching the whole suite stay green.
//
// WHY IT ESCAPED BOTH DETECTORS. citedAsOpen let the state word follow the
// number across whitespace, one punctuation mark and an opening bracket, which
// is every shape the two rotted sentences happened to use — and nothing else,
// so a single verb in between ended the match. The other detector could not
// cover for it: outstandingPhrases is a list of whole phrases precisely because
// "still" and "open" do honest work in these documents, and neither "still
// open" nor "is open" can be added to it without firing on "the vocabulary is
// still growing" and "the fail-open defect class".
//
// SO THE CONNECTOR IS A CLOSED VOCABULARY, NOT A WILDCARD. Allowing arbitrary
// words between the number and the state word would read "#268 and the
// open-source cut" as a state claim. What is allowed is the copulas an English
// sentence uses to attach a state to a subject — is/was/are/were/remains/stays
// — which is the whole of the gap and none of the surrounding prose.
func TestStaleClaimDetectorFiresOnACopulaBetweenTheNumberAndItsState(t *testing.T) {
	for _, tc := range []struct{ name, doc string }{
		{"the sentence planted in SECURITY.md", "There is no flag for any of them. Issue #146 is still open.\n"},
		{"the copula alone", "The subdirectory refusal is #150. Issue #179 is open.\n"},
		{"a comma-delimited citation with remains", "The include-path refusal, #173, remains unresolved.\n"},
		{"an em-dash aside in the past tense", "The wiring take-over — #146 — was outstanding for thirteen days.\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := staleIssueClaims(tc.doc); len(got) == 0 {
				t.Fatalf("the detector passed an issue-state claim:\n%s", tc.doc)
			}
		})
	}
}

// TestStaleClaimDetectorLeavesHonestProseAlone is the other half. A detector
// that refuses everything passes the fire cases just as well, and this
// repository's published documents cite issue numbers 56 times for provenance.
// Every string here is real text from the tree.
func TestStaleClaimDetectorLeavesHonestProseAlone(t *testing.T) {
	cases := []struct{ name, doc string }{
		// The four below are the exclusivity half of the copula widening
		// above: each puts a state word downstream of an issue number with
		// something other than a copula in between, and each is a shape the
		// tree really carries.
		{"a sentence boundary is not a copula", "Follow #113. It is open to anyone who has read CONTRIBUTING.md.\n"},
		{"a conjunction is not a copula", "That was #268 and the open-source cut landed with it.\n"},
		{"an origin field quoting a wound", "  origin: \"…#412\"              # what wound this rule closed\n"},
		{"a copula with no state word after it", "\"built-in analyzers\" is **done** (#118), and the list is growing.\n"},
		{"a plain citation", "It refuses rather than guessing, and there is no flag for that\neither. Issue #173.\n"},
		{"a decision reference", "so it is machine-independent and meant to be committed (#146 D4).\n"},
		{"an arc reference", "Two fixes in the #142/#143/#146 arc were right-idea/wrong-altitude.\n"},
		{"the defect class, which contains the word open", "  (#150). `fail-open-scout` runs this pass for you.\n"},
		{"open used as a verb near a citation", "Please open an issue first and get a maintainer's agreement. Issue #113.\n"},
		{"still, doing honest work", "the rule-type vocabulary is still growing as real corpora demand it (#126).\n"},
		{"a limitation with no issue attached", "The `command` rule type is not yet built.\n"},
		{"a citation a paragraph away from a limitation", "Issue #167.\n\nThe `ordering` type is not yet built.\n"},
		{"a citation beyond the window in the same paragraph", "Issue #167. " + strings.Repeat("filler words here. ", 20) + "It is not yet built.\n"},
		{"pending used as a denial", "\"built-in analyzers\" is **done**, not pending — `goast`, `dartscan` (#118).\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := staleIssueClaims(tc.doc); len(got) != 0 {
				t.Errorf("the detector refused honest prose %+v:\n%s", got, tc.doc)
			}
		})
	}
}

// TestPublishedDocSetStopsAtTheAgentInstructions pins the third scope
// judgement. `git ls-files -- *.md` matches at any depth, so every SKILL.md and
// every agent definition under `.claude/` reaches isPublishedDocPath and is
// turned away there. Nothing said why, and #323 asked for the exclusion to be
// reversed — an unexplained exclusion is one edit from being read as an
// oversight and "fixed".
//
// IT IS NOT AN OVERSIGHT. The live agent instructions are governed by `make
// agent-doc-paths`, a verify prerequisite that derives its subject from `git
// ls-files -- '.claude'`, resolves every inline code span's path citation
// against what git tracks, and refuses a tree it cannot read rather than
// passing over nothing. Reaching the same files from here costs two things.
//
// TWO SEMANTICS OVER ONE SURFACE, measurably different. This file's scanner
// skips fenced blocks (TestCitationScannerStopsAtFencedBlocks pins that the
// exclusion changes the answer) because docs/quickstart.md walks a reader
// through a repository that is not this one; an agent instruction has no
// example's world, so agent-doc-paths reads fences deliberately. They also
// disagree about a line suffix: widened, TestPublishedDocsCiteNoMissingFile
// condemns .claude/output-styles/operator.md:37, whose placeholder token is the
// sentence teaching an agent how to cite code — this scanner strips the line
// number and resolves the rest, agent-doc-paths refuses a token carrying a
// colon. Of two verdicts over one surface, the weaker is the one authors learn
// to write for.
//
// AND IT WOULD BREAK THE CUT. tools/publication/exclude.txt drops `.claude`
// whole, and Tier 6's rule is that a package whose tests read a dropped path
// leaves with it. internal/repoproof does not leave. A published set reaching
// into `.claude` would either judge nothing there — vacuous, and silent about
// it — or carry a floor entry naming a file the cut does not have.
func TestPublishedDocSetStopsAtTheAgentInstructions(t *testing.T) {
	// The synthetic arm. It holds whatever the tree contains, which is the
	// point: the exclusion is a property of the predicate, not a fact about
	// today's checkout, and it must stay judged in the public cut where
	// `.claude` is gone.
	for _, p := range []string{
		".claude/skills/planning/SKILL.md",
		".claude/agents/fail-open-scout.md",
		".claude/output-styles/operator.md",
	} {
		if isPublishedDocPath(p) {
			t.Errorf("%s reads as a published document.\n%s", p, agentInstructionCure)
		}
	}
	// The tree arm, for a spelling no synthetic case anticipated.
	for _, d := range publishedDocs(t) {
		if strings.HasPrefix(filepath.ToSlash(d), ".claude/") {
			t.Errorf("the published-document set reached %s.\n%s", d, agentInstructionCure)
		}
	}
}

const agentInstructionCure = "" +
	"The live agent instructions are judged by `make agent-doc-paths`, a verify\n" +
	"prerequisite that derives its subject from `git ls-files -- '.claude'`. This gate is\n" +
	"its complement, not its replacement: the two disagree on purpose about fenced blocks\n" +
	"and about a `file.go:42` line suffix, and tools/publication/exclude.txt drops\n" +
	"`.claude` from the OSS cut while keeping this package, so a published set that\n" +
	"reached it would go red on the public repository's first push. Extend\n" +
	"agent-doc-paths instead — AGENTS.md records why the rule engine holds neither end."

// docPathDecisionAnchor is how the record is found: the one path only it cites,
// and — being a citation in a published document — one
// TestPublishedDocsCiteNoMissingFile resolves against the tree.
const docPathDecisionAnchor = "internal/rules/docpathexists/docpathexists.go"

// docPathDecisionNames are what the record must name for a reader to be able to
// check it: the type, both live gates, and the difference it turns on.
var docPathDecisionNames = []string{
	"doc-path-exists",
	"internal/repoproof/doc_path_reference_test.go",
	"agent-doc-paths",
	"git ls-files",
	"os.Stat",
}

var docPathRuleDecl = regexp.MustCompile(`(?m)^\s*(?:-\s*)?type:\s*doc-path-exists\s*$`)

// TestAGENTSRecordsTheDocPathExistsDecision pins the second half of #323. The
// engine registers a `doc-path-exists` rule type and examples/ uses it, so the
// obvious reading of that issue is that this repository should point the rule
// at its own documents. It does not, and an unexplained absence is one
// confident agent away from being closed as an oversight.
//
// The record is only worth the facts under it, so three things are checked
// together: that no rule of that type is declared in this repository's own
// `.formwork/`; that the type still resolves with os.Stat against the
// filesystem and still never asks git — the whole reason both live gates are
// code, since a citation satisfied by an untracked scratch file passes for its
// author and dangles for the next clone; and that AGENTS.md carries the record
// in one block naming both gates.
//
// THE ABSENT CASE ASSERTS TOO. In the OSS cut, AGENTS.md is materialised from
// tools/publication/public-AGENTS.md and `.claude/` does not cross, so the
// record would be a claim about a surface the reader does not have. Where no
// agent instruction is tracked the record must be ABSENT — a branch that
// decides nothing is the thing this file exists to refuse.
func TestAGENTSRecordsTheDocPathExistsDecision(t *testing.T) {
	root := repoRoot(t)
	for _, f := range trackedFiles(t, root, ".formwork") {
		body, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			t.Fatalf("cannot read %s: %v", f, err)
		}
		if docPathRuleDecl.Match(body) {
			t.Errorf("%s declares a doc-path-exists rule, and AGENTS.md records that this repository runs none.\n"+
				"Either the record is now false and must be rewritten with the reason it changed, or the rule is the oversight.", f)
		}
	}
	engine, err := os.ReadFile(filepath.Join(root, docPathDecisionAnchor))
	if err != nil {
		t.Fatalf("cannot read the rule type the record is about: %v", err)
	}
	if !strings.Contains(string(engine), "os.Stat(") || strings.Contains(string(engine), "ls-files") {
		t.Errorf("%s no longer resolves citations the way AGENTS.md's record says it does.\n"+
			"The record turns on that difference — the live gates resolve against `git ls-files`, this resolves\n"+
			"against the filesystem — so a change here is a change to the decision, not just to the code.",
			docPathDecisionAnchor)
	}

	agents, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatalf("cannot read AGENTS.md: %v", err)
	}
	var record string
	for _, b := range blocks(string(agents)) {
		if strings.Contains(b.text, docPathDecisionAnchor) {
			record = b.text
			break
		}
	}
	var instructions []string
	for _, p := range trackedFiles(t, root, ".claude") {
		if strings.HasSuffix(p, ".md") {
			instructions = append(instructions, p)
		}
	}
	if len(instructions) == 0 {
		if record != "" {
			t.Fatalf("AGENTS.md records the doc-path-exists decision, but this tree tracks no agent instruction under .claude/.\n" +
				"Half that record is about `make agent-doc-paths` and the files it judges. In a tree without them it is a claim\n" +
				"the reader cannot check and cannot act on.")
		}
		return
	}
	if record == "" {
		t.Fatalf("AGENTS.md does not record why no doc-path-exists rule governs this repository's own documents.\n"+
			"%d agent instruction(s) are tracked under .claude/ and the type is registered, so the absence reads as an\n"+
			"oversight until the reason is written down. The record is one block citing %s, and naming: %s.",
			len(instructions), docPathDecisionAnchor, strings.Join(docPathDecisionNames, ", "))
	}
	for _, name := range docPathDecisionNames {
		if !strings.Contains(record, name) {
			t.Errorf("AGENTS.md's doc-path-exists record does not name %q.\n"+
				"A reader who cannot follow the record to the gate that replaced the rule cannot check the decision.", name)
		}
	}
}

// verifyRestatement is a `+`-joined run of target names, wrapping allowed: how
// AGENTS.md spells out what `make verify` depends on.
var verifyRestatement = regexp.MustCompile(`[a-z][a-z0-9-]*(?:[ \t\n]*\+[ \t\n]*[a-z][a-z0-9-]*)+`)

// restatedVerifyPrereqs returns the prerequisites a document writes out in its
// `make verify` bullet, or nothing where it points at the derived listing
// instead. Every block naming the target is searched, not the first: these
// documents name `make verify` in prose long before the bullet describing it.
func restatedVerifyPrereqs(doc string) []string {
	for _, b := range blocks(doc) {
		i := strings.Index(b.text, "`make verify`")
		if i < 0 {
			continue
		}
		run := verifyRestatement.FindString(b.text[i:])
		if run == "" {
			continue
		}
		var names []string
		for _, f := range strings.Split(run, "+") {
			names = append(names, strings.TrimSpace(f))
		}
		return names
	}
	return nil
}

// TestAGENTSVerifyListMatchesTheMakefile closes a drift of exactly the shape
// this file exists for. README.md and tools/publication/public-AGENTS.md both
// refuse to copy verify's prerequisites out — "it reads the Makefile, so unlike
// a list written out here it cannot fall behind" — and AGENTS.md copies them
// anyway. Two prerequisites later (corpus-disclosure-proof, agent-doc-paths)
// the copy said something false about what CI runs.
//
// The copy is pinned rather than banned: the list is worth reading in place.
// Both branches assert. Where the bullet enumerates, the enumeration is exactly
// the Makefile's set; where it does not — the OSS cut materialises AGENTS.md
// from the public overlay, which points at `make help` — the bullet must still
// hand the reader that derived listing.
func TestAGENTSVerifyListMatchesTheMakefile(t *testing.T) {
	if got := restatedVerifyPrereqs("- `make verify` — the full gate CI runs. `make help` lists every\n  target with its description.\n"); len(got) != 0 {
		t.Errorf("read %v as an enumeration out of a bullet that enumerates nothing", got)
	}
	if got := restatedVerifyPrereqs("- `make verify` — everything CI would run: test + vet +\n  fmt.\n"); strings.Join(got, " ") != "test vet fmt" {
		t.Errorf("the extractor read %q out of a wrapped three-name enumeration", strings.Join(got, " "))
	}

	agents, err := os.ReadFile(filepath.Join(repoRoot(t), "AGENTS.md"))
	if err != nil {
		t.Fatalf("cannot read AGENTS.md: %v", err)
	}
	restated := restatedVerifyPrereqs(string(agents))
	if len(restated) == 0 {
		for _, b := range blocks(string(agents)) {
			if strings.Contains(b.text, "`make verify`") && strings.Contains(b.text, "`make help`") {
				return
			}
		}
		t.Fatal("AGENTS.md neither lists verify's prerequisites nor points at `make help`, " +
			"so a reader learns nothing about what the gate runs")
	}
	want := map[string]bool{}
	for _, p := range readMakefileFacts(t).verifyPrereqs {
		want[p] = true
	}
	if len(want) == 0 {
		t.Fatal("parsed no prerequisites from the Makefile's `verify:` line, so this gate has nothing to judge against")
	}
	got := map[string]bool{}
	for _, n := range restated {
		got[n] = true
		if !want[n] {
			t.Errorf("AGENTS.md lists %q as a verify prerequisite; the Makefile's verify line does not.", n)
		}
	}
	for p := range want {
		if !got[p] {
			t.Errorf("`make verify` depends on %q and AGENTS.md's list omits it.\n"+
				"That list is what an agent reads to learn what CI runs. Restate it from the Makefile's verify line, "+
				"or drop the copy and point at `make help` the way public-AGENTS.md does.", p)
		}
	}
}
