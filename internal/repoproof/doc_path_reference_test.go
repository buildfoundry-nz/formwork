// doc_path_reference_test.go — when a published document points at a file in
// this tree, the file is there.
//
// WHY THIS EXISTS. A path citation is the one kind of documentation claim that
// goes false without anybody editing the sentence. Delete a file and every
// sentence naming it rots at once, silently, in documents nobody opened that
// week. That is not hypothetical here: a8503fd9 removed the whole scripts/
// directory, rewired the Makefile, both workflows and one agent skill, and left
// a second skill telling agents to run `scripts/panel-for-plan.sh` — a file that
// had stopped existing in the same commit. The reader gets "no such file or
// directory" and no pointer to what replaced it.
//
// Nothing in the repository could see that, which is the part worth fixing.
// formwork registers a `doc-path-exists` rule type and three rules in examples/
// use it, so the engine has held this class for adopters since before it held it
// for itself. This gate closes that asymmetry for the published documents.
//
// WHAT IS JUDGED, AND WHY EACH BOUNDARY IS WHERE IT IS. All four boundaries are
// structural — none of them is a list of paths to forgive, because a list of
// paths to forgive is how a gate stops holding without anybody deciding to stop.
//
//   - INLINE CODE SPANS ONLY. A fenced block is a transcript or a config
//     example: docs/quickstart.md walks the reader through building a rule in a
//     repository that is not this one, so its `internal/handler/orders.go` names
//     a file in the tutorial's tree, and docs/reference.md's YAML shows what an
//     `except:` clause looks like, not where an allowlist lives. Paths inside a
//     fence belong to the example's world. An inline span in prose is the author
//     pointing at something here. TestCitationScannerStopsAtFencedBlocks pins
//     that the distinction is load-bearing rather than decorative.
//
//   - A FILENAME, NOT A REGION. The token's last segment must read as a file:
//     a name with a lowercase extension. `tools/parity` is documented three
//     words later in AGENTS.md as "a design name, not a path", and README's
//     `.claude/worktrees/` is an example of a noisy tree in the ADOPTER's
//     repository. Both are regions, both are honest prose, and a gate that
//     refused them would be teaching authors to write around it. A file
//     citation is different in kind: it is a pointer the reader is expected to
//     open, and it is the shape that rotted.
//
//   - A PATH, NOT A PATTERN OR A SYMBOL. A token carrying a glob or a
//     placeholder (`scripts/check-*.sh`, `.formwork/fixtures/<rule-id>/`) names
//     a set, not a file. A token whose last segment is a Go qualified symbol
//     (`internal/vcs.IgnoredUnder` — no file extension begins with a capital)
//     names an identifier. A hostname-shaped first segment
//     (`github.com/wasilibs/go-pgquery`) names a module.
//
//   - RESOLVED AGAINST THREE ROOTS, all derived. This tree; any corpus shipped
//     under examples/, because these documents describe adopter trees and
//     `.formwork/lint.yaml` is a per-corpus file every ported corpus carries;
//     and git's own ignore rules, because `dist/metadata.json` is what
//     GoReleaser writes and `projects/` is what `make sync` materialises — the
//     repository has already declared those trees to be output rather than
//     source, and the gate reads that declaration instead of keeping its own.
//
// THIS IS A TEST RATHER THAN A FORMWORK RULE, for the same reason
// doc_issue_state_test.go is: the judged set is derived from git — see
// publishedDocs there — so a document added or renamed is covered the day it
// lands, where a rule would carry that set as a literal glob list and a glob
// list that stops matching PASSES. The resolution arms need the filesystem and
// git's ignore rules, which is a test's job rather than a content pattern's.
package repoproof_test

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// citedPath is one path token an inline code span points at, kept with its
// origin so an unresolved citation reports the citing file:line.
type citedPath struct {
	doc   string
	line  int
	token string
}

// citedRepoFiles returns every token in doc that reads as a repo-relative file
// path. includeFences decides whether fenced-block contents are read; the gate
// passes false, and TestCitationScannerStopsAtFencedBlocks passes true to prove
// the exclusion changes the answer.
func citedRepoFiles(doc string, includeFences bool) []citedPath {
	var out []citedPath
	inFence := false
	for i, line := range strings.Split(doc, "\n") {
		if fenceDelimiter.MatchString(line) {
			inFence = !inFence
			continue
		}
		var spans []string
		switch {
		case inFence && includeFences:
			spans = []string{line}
		case inFence:
			continue
		default:
			for _, m := range inlineSpan.FindAllStringSubmatch(line, -1) {
				spans = append(spans, m[1])
			}
		}
		for _, span := range spans {
			for _, field := range strings.Fields(span) {
				if token, ok := repoFileToken(field); ok {
					out = append(out, citedPath{line: i + 1, token: token})
				}
			}
		}
	}
	return out
}

var (
	// inlineSpan is one backtick-delimited run within a single line. Markdown's
	// inline code does not cross a line, and refusing to cross one here keeps a
	// stray backtick in prose from swallowing the rest of a document.
	inlineSpan = regexp.MustCompile("`([^`]+)`")

	// fenceDelimiter opens or closes a fenced block.
	fenceDelimiter = regexp.MustCompile("^[ \t]*(?:```|~~~)")

	// lineSuffix is the ":59" a citation carries when it points at a line
	// inside the file it names.
	lineSuffix = regexp.MustCompile(`:[0-9]+$`)

	// fileLeaf is the shape of a filename: a name carrying a lowercase
	// extension. It is what separates a file citation from a region — `tools/`,
	// `internal/rules/`, `.claude/worktrees/` — and from a Go qualified symbol,
	// because no file extension in this tree begins with a capital, so
	// `internal/vcs.IgnoredUnder` cannot pass it.
	fileLeaf = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*\.[a-z][a-z0-9]*$`)

	// hostLeadSegment is a hostname in the first segment: `github.com/wasilibs/…`
	// names a module, and a module is resolved by the toolchain rather than by
	// looking in this tree.
	hostLeadSegment = regexp.MustCompile(`^[a-z0-9-]+(?:\.[a-z0-9-]+)+$`)
)

// tokenMetacharacters are the characters that say a token names something other
// than one file in this tree: a glob or a placeholder names a set, an
// assignment or a redirect names a command line, and a `~` or a `:` names
// another machine's tree or another kind of address entirely.
const tokenMetacharacters = "*?<>{}[]()%$|=!~#^&\\\"';:,"

// citationTrailEdges are the characters a citation picks up from the sentence
// that closes around it. They are trimmed from the end only: a metacharacter in
// the MIDDLE of a token is what disqualifies it, and trimming there would
// manufacture a path the document never wrote.
const citationTrailEdges = ".,;:\"'"

// citationLeadEdges is deliberately quotes alone. A leading dot is part of the
// name — `.formwork/formwork.yaml`, `.claude/settings.json` — and trimming it
// rewrites the citation into a different path that git happens to ignore, which
// is a silent pass over a document nobody checked.
const citationLeadEdges = "\"'"

// repoFileToken decides whether one whitespace-delimited field of a code span
// is a repo-relative file citation, and returns it normalised.
func repoFileToken(field string) (string, bool) {
	token := strings.TrimRight(strings.TrimLeft(field, citationLeadEdges), citationTrailEdges)
	token = lineSuffix.ReplaceAllString(token, "")
	token = strings.TrimRight(token, citationTrailEdges)
	token = strings.TrimPrefix(token, "./")

	switch {
	case !strings.Contains(token, "/"),
		strings.HasPrefix(token, "/"),
		strings.Contains(token, ".."),
		strings.Contains(token, "//"),
		strings.ContainsAny(token, tokenMetacharacters):
		return "", false
	}
	segments := strings.Split(token, "/")
	if hostLeadSegment.MatchString(segments[0]) {
		return "", false
	}
	if !fileLeaf.MatchString(segments[len(segments)-1]) {
		return "", false
	}
	return token, true
}

// unresolvedCitations returns the citations that resolve against none of the
// three roots: the repository at root, any corpus shipped under examples/, and
// the trees git is told to ignore.
func unresolvedCitations(root string, cites []citedPath) ([]citedPath, error) {
	corpora, err := filepath.Glob(filepath.Join(root, "examples", "*"))
	if err != nil {
		return nil, fmt.Errorf("cannot enumerate the corpora under examples/: %w", err)
	}

	// A token is usually cited many times; resolve each distinct one once.
	resolved := map[string]bool{}
	var absent []string
	for _, c := range cites {
		if _, seen := resolved[c.token]; seen {
			continue
		}
		onDisk, err := citationOnDisk(root, corpora, c.token)
		if err != nil {
			return nil, err
		}
		resolved[c.token] = onDisk
		if !onDisk {
			absent = append(absent, c.token)
		}
	}

	// Whatever is on no disk gets one question to git: has this repository
	// already declared the tree it names to be output rather than source?
	if len(absent) > 0 {
		ignored, err := gitIgnores(root, absent)
		if err != nil {
			return nil, err
		}
		for token := range ignored {
			resolved[token] = true
		}
	}

	var missing []citedPath
	for _, c := range cites {
		if !resolved[c.token] {
			missing = append(missing, c)
		}
	}
	return missing, nil
}

// citationOnDisk looks for token under the repository root and then under each
// corpus shipped in examples/. A stat failure that is not "absent" is returned
// rather than read as absence: a gate that cannot look must not answer
// "nothing there".
func citationOnDisk(root string, corpora []string, token string) (bool, error) {
	rel := filepath.FromSlash(token)
	for _, base := range append([]string{root}, corpora...) {
		_, err := os.Stat(filepath.Join(base, rel))
		switch {
		case err == nil:
			return true, nil
		case errors.Is(err, os.ErrNotExist):
		default:
			return false, fmt.Errorf("resolving %q under %s: %w", token, base, err)
		}
	}
	return false, nil
}

// gitIgnores asks git which of these paths the repository has declared to be
// ignored. check-ignore exits 1 when none of them is, which is an answer;
// every other failure means git could not answer, and is returned.
func gitIgnores(root string, tokens []string) (map[string]bool, error) {
	cmd := exec.Command("git", "check-ignore", "-z", "--stdin")
	cmd.Dir = root
	cmd.Stdin = strings.NewReader(strings.Join(tokens, "\x00") + "\x00")
	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 {
			return nil, fmt.Errorf("git check-ignore could not answer for %d path(s): %w", len(tokens), err)
		}
	}
	ignored := map[string]bool{}
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" {
			ignored[filepath.ToSlash(p)] = true
		}
	}
	return ignored, nil
}

// --- the gate ---------------------------------------------------------------

// publishedCitations scans every published document for the file paths its
// prose points at. The document set is publishedDocs — derived from git in
// doc_issue_state_test.go — so a document added or renamed is judged the day it
// lands.
func publishedCitations(t *testing.T) []citedPath {
	t.Helper()
	root := repoRoot(t)
	var all []citedPath
	for _, rel := range publishedDocs(t) {
		body, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("cannot read %s, so this gate cannot answer for it: %v", rel, err)
		}
		for _, c := range citedRepoFiles(string(body), false) {
			c.doc = rel
			all = append(all, c)
		}
	}
	return all
}

const citationCure = "" +
	"A published document that points at a file is making a claim the reader will act on.\n" +
	"Follow the citation to where the file went and rewrite it — `scripts/panel-for-plan.sh`\n" +
	"became `go run ./cmd/panel-for-plan` when scripts/ was deleted whole in a8503fd9, and the\n" +
	"skill that kept the old spelling sent readers to a file that had stopped existing.\n" +
	"Never create the file to satisfy this gate, and never demote the citation out of a code\n" +
	"span to hide it: if the token is not a path, say so in the sentence the way AGENTS.md does\n" +
	"for tools/parity."

// TestPublishedDocsCiteNoMissingFile is the gate. It reads the real tree.
func TestPublishedDocsCiteNoMissingFile(t *testing.T) {
	needBinary(t, "git")
	cites := publishedCitations(t)
	if len(cites) == 0 {
		t.Fatal("no path citation resolved out of the published documents — this gate would pass over nothing")
	}
	missing, err := unresolvedCitations(repoRoot(t), cites)
	if err != nil {
		t.Fatalf("cannot resolve the cited paths, so this gate cannot answer: %v", err)
	}
	if len(missing) > 0 {
		var report []string
		for _, c := range missing {
			report = append(report, "  "+c.doc+":"+strconv.Itoa(c.line)+"  cites a file that is not there — "+c.token)
		}
		t.Fatalf("%d published citation(s) point at nothing:\n%s\n\n%s",
			len(missing), strings.Join(report, "\n"), citationCure)
	}
}

func TestPublishedCitationsCoverTheDocumentsThatPointIntoTheTree(t *testing.T) {
	got := map[citedPath]bool{}
	for _, c := range publishedCitations(t) {
		got[citedPath{doc: c.doc, token: c.token}] = true
	}
	floor := append([]citedPath(nil), citationFloor...)
	if isDevelopmentTree(t) {
		floor = append(floor, citationFloorDev...)
	} else {
		floor = append(floor, citationFloorCut...)
	}
	var missing []string
	for _, want := range floor {
		if !got[want] {
			missing = append(missing, want.doc+" -> "+want.token)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("the scanner no longer sees %d citation(s) this gate is meant to judge:\n  %s\n"+
			"If the pointer moved, follow it in citationFloor. If the scanner stopped seeing it, that is the bug.",
			len(missing), strings.Join(missing, "\n  "))
	}
}

// TestCitationScannerStopsAtFencedBlocks pins the scope judgement that a fenced
// block belongs to the example's world, and pins it non-vacuously: the first arm
// fails if docs/quickstart.md has stopped carrying a tutorial path that does not
// resolve here, because then the exclusion would be doing no work and nobody
// would know it had rotted.
func TestCitationScannerStopsAtFencedBlocks(t *testing.T) {
	needBinary(t, "git")
	root := repoRoot(t)
	body, err := os.ReadFile(filepath.Join(root, "docs/quickstart.md"))
	if err != nil {
		t.Fatalf("cannot read the quickstart, so this exclusion is untested: %v", err)
	}
	fenced, err := unresolvedCitations(root, citedRepoFiles(string(body), true))
	if err != nil {
		t.Fatalf("cannot resolve the quickstart's citations: %v", err)
	}
	if len(fenced) == 0 {
		t.Fatal("the quickstart's fenced walkthrough no longer names a path outside this tree, " +
			"so the fenced-block exclusion is untested — point this test at a document whose fences still do")
	}
	prose, err := unresolvedCitations(root, citedRepoFiles(string(body), false))
	if err != nil {
		t.Fatalf("cannot resolve the quickstart's prose citations: %v", err)
	}
	if len(prose) != 0 {
		t.Errorf("the fenced-block exclusion let %d example path(s) through: %+v", len(prose), prose)
	}
}

// --- the resolver's three roots, each exercised -----------------------------

// TestCitationResolvesAgainstAShippedCorpus pins the corpus arm. `.formwork/lint.yaml`
// is a per-corpus declaration AGENTS.md describes generically; it is not a file
// at this repository's root, and judging it against this root alone would refuse
// an honest sentence.
func TestCitationResolvesAgainstAShippedCorpus(t *testing.T) {
	needBinary(t, "git")
	root := repoRoot(t)
	const perCorpus = ".formwork/lint.yaml"
	if _, err := os.Stat(filepath.Join(root, perCorpus)); err == nil {
		t.Fatalf("%s now exists at this repository's root, so it no longer exercises the corpus arm — "+
			"point this test at a file that is still corpus-only", perCorpus)
	}
	corpora, err := filepath.Glob(filepath.Join(root, "examples", "*", perCorpus))
	if err != nil || len(corpora) == 0 {
		t.Fatalf("no shipped corpus carries %s, so the corpus arm is vacuous here (err %v)", perCorpus, err)
	}
	got, err := unresolvedCitations(root, []citedPath{{doc: "AGENTS.md", line: 183, token: perCorpus}})
	if err != nil {
		t.Fatalf("cannot resolve %s: %v", perCorpus, err)
	}
	if len(got) != 0 {
		t.Errorf("the corpus arm did not resolve %s, which %d shipped corpora carry", perCorpus, len(corpora))
	}
}

// TestCitationResolvesAgainstAnIgnoredTree pins the ignore arm, and pins that it
// is narrow: it forgives what git has been told is output, and nothing else.
func TestCitationResolvesAgainstAnIgnoredTree(t *testing.T) {
	needBinary(t, "git")
	root := repoRoot(t)
	const output = "dist/metadata.json"
	if _, err := os.Stat(filepath.Join(root, output)); err == nil {
		t.Fatalf("%s is present, so it no longer exercises the ignore arm", output)
	}
	got, err := unresolvedCitations(root, []citedPath{{doc: "AGENTS.md", line: 220, token: output}})
	if err != nil {
		t.Fatalf("cannot resolve %s: %v", output, err)
	}
	if len(got) != 0 {
		t.Errorf("the ignore arm did not resolve %s, which .gitignore declares to be build output", output)
	}

	const absent = "internal/nosuchpackage/nosuchfile.go"
	got, err = unresolvedCitations(root, []citedPath{{doc: "AGENTS.md", line: 1, token: absent}})
	if err != nil {
		t.Fatalf("cannot resolve %s: %v", absent, err)
	}
	if len(got) != 1 {
		t.Errorf("the resolver forgave %s, which git does not ignore and which is not there: %+v", absent, got)
	}
}

// --- the scanner's own refusals, exercised rather than assumed --------------

// TestCitationScannerFindsTheReferenceThatWentDangling feeds the scanner the
// exact sentence the planning skill carried after scripts/ was deleted, and then
// resolves it against the real tree, so both halves of the gate are shown firing
// on the reference that started this.
func TestCitationScannerFindsTheReferenceThatWentDangling(t *testing.T) {
	needBinary(t, "git")
	const doc = "The plan must open with a fenced `plan-size` block holding one `lines: N` line,\n" +
		"then a closing fence. `scripts/panel-for-plan.sh` reads it to choose the review\n" +
		"panel, so prose is not enough.\n"
	got := citedRepoFiles(doc, false)
	if len(got) != 1 {
		t.Fatalf("the scanner found %d citation(s) in the sentence that rotted, want exactly 1: %+v", len(got), got)
	}
	if got[0].token != "scripts/panel-for-plan.sh" || got[0].line != 2 {
		t.Fatalf("scanner reported %+v, want scripts/panel-for-plan.sh at line 2", got[0])
	}
	missing, err := unresolvedCitations(repoRoot(t), got)
	if err != nil {
		t.Fatalf("cannot resolve the dangling citation: %v", err)
	}
	if len(missing) != 1 {
		t.Fatalf("the resolver reported %d unresolved citation(s) for a file this repository deleted, want 1", len(missing))
	}
}

// TestCitationScannerReadsAFileReferenceWithALineNumber pins that a citation
// carrying the line it points at is still judged as the file it names.
func TestCitationScannerReadsAFileReferenceWithALineNumber(t *testing.T) {
	got := citedRepoFiles("recorded at `docs/parity/PARITY-STATUS.md:59` once before.\n", false)
	if len(got) != 1 || got[0].token != "docs/parity/PARITY-STATUS.md" {
		t.Fatalf("scanner reported %+v, want docs/parity/PARITY-STATUS.md", got)
	}
}

// TestCitationScannerKeepsTheLeadingDotOfADotDirectory pins the half of the
// edge-trimming that a symmetric trim gets wrong, and it is here because a
// symmetric trim is what this scanner shipped to its own gate first.
// `.claude/settings.json` trimmed to `claude/settings.json` is simply a
// different path — but `.formwork/formwork.yaml` trimmed to
// `formwork/formwork.yaml` lands under the name .gitignore gives the built
// binary, so git calls it ignored, the resolver forgives it, and the gate
// reports green over a citation it never checked. The second arm pins that
// hazard rather than describing it: if the mis-trimmed form ever stops being
// forgiven, the trim no longer needs guarding and this test should say so.
func TestCitationScannerKeepsTheLeadingDotOfADotDirectory(t *testing.T) {
	needBinary(t, "git")
	root := repoRoot(t)
	for _, want := range []string{".claude/settings.json", ".formwork/formwork.yaml"} {
		got := citedRepoFiles("declared in `"+want+"`, once.\n", false)
		if len(got) != 1 || got[0].token != want {
			t.Errorf("scanner reported %+v for `%s`, want the citation unchanged", got, want)
		}
	}

	const misTrimmed = "formwork/formwork.yaml"
	if _, err := os.Stat(filepath.Join(root, misTrimmed)); err == nil {
		t.Fatalf("%s exists, so it no longer demonstrates the hazard — pick a dot-directory whose trimmed form is still ignored", misTrimmed)
	}
	swallowed, err := unresolvedCitations(root, []citedPath{{doc: "CLAUDE.md", token: misTrimmed}})
	if err != nil {
		t.Fatalf("cannot resolve %s: %v", misTrimmed, err)
	}
	if len(swallowed) != 0 {
		t.Errorf("%s is no longer forgiven by the ignore arm, so the leading-dot trim is no longer silent — "+
			"re-read TestCitationScannerKeepsTheLeadingDotOfADotDirectory's premise", misTrimmed)
	}
}

// TestCitationScannerLeavesEverythingElseAlone is the other half. A scanner that
// judged everything would pass the fire case just as well. Every span here is
// real text from this repository's published documents.
func TestCitationScannerLeavesEverythingElseAlone(t *testing.T) {
	cases := []struct{ name, span string }{
		{"a design name the sentence says is not a path", "`tools/parity` is a design name, not a path"},
		{"a directory in the adopter's repository", "worktrees under `.claude/worktrees/`, vendored source"},
		{"a Go qualified symbol", "the shared `internal/vcs.IgnoredUnder` helper"},
		{"a glob over the validating target", "its `scripts/check-*.sh` are the rules"},
		{"a doublestar exclude", "`scope.exclude \"**/*_test.go\" matches no files`"},
		{"a home-relative checkout", "cloned under `~/src/bf/pinnedrepo`"},
		{"a module path", "`github.com/wasilibs/go-pgquery` is CGo-free"},
		{"a command line", "run `go build ./cmd/formwork` first"},
		{"a make invocation carrying a variable", "`make test PKG=./internal/engine`"},
		{"a shebang", "a `#!/bin/sh` shim"},
		{"a YAML clause showing an allowlist", "`except: {allowlist: allowlists/legacy.txt}`"},
		{"a relative-parent example", "a scope entry of `x/..`"},
		{"a fixture template", "`.formwork/fixtures/<rule-id>/` holds them"},
		{"a verdict line", "prints `0/0 rules passed`"},
		{"a rule-type lane", "`go/call-order-in-func` and `sql/locking-target`"},
		{"a bare directory", "`internal/rules/` holds one package per type"},
		{"a slash-free span", "`formwork check` exits 1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := citedRepoFiles(tc.span+"\n", false); len(got) != 0 {
				t.Errorf("the scanner judged honest prose as a file citation %+v:\n%s", got, tc.span)
			}
		})
	}
}
