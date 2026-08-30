// locking_coverage_test.go — #313.
//
// docs/reference.md tells adopters, of the COVERAGE LIMIT block in locking.go:
// "See the `COVERAGE LIMIT` block in `internal/rules/sqlparse/locking.go` for
// the current list — it is maintained as a checkable claim, not as prose."
// Nothing checked it. It spent a day declaring #72 and #73 OPEN and unmodelled
// after both landed, and it named for the forward `goto` a behaviour — "a
// forward jump's skipped append is silently included" — that is the exact
// opposite of what the shipped rule does: the jumped-over world IS emitted and
// the rule FIRES. An adopter looking at a real forward-goto finding was told by
// the document the manual points them at to dismiss it as a known artifact.
//
// The drift was structural, not careless. #74's commit wrote those lines at
// 07:53; #73 landed at 08:01 and #72 at 08:07, each touching internal/sqlextract
// and neither touching this file. Prose that no test reads cannot be kept in
// step by remembering to keep it in step.
//
// So the block now carries a machine-readable half — one SHAPE line per
// disclosed composition — and this test runs every one of those shapes through
// the REAL rule and fails unless the verdict is the one the block discloses.
// Drift reddens in BOTH directions: a shape whose behaviour changed fails, and
// so does a disclosure with no shape behind it.
//
// THREE VERDICTS, and telling the last two apart is the block's whole purpose:
//
//   - FIRES   the rule reports the hazard on this composition.
//   - PASSES  a folded world was built and the rule judged it safe. A clean run
//     here is an answer.
//   - SILENT  no folded world exists at all. A clean run here means "not
//     analysed", which is what the block exists to say out loud.
//
// A match count alone cannot separate PASSES from SILENT — both are zero
// findings — so each shape is run through sqlextract.FromGoReassembled as well.
// That is also why a "0 -> 0" fix like #310's could not be pinned at this layer
// before: the difference is which candidates were emitted, not how many
// findings came back.
//
// Method follows internal/meta/rule_authoring_doc_test.go, which reads live
// lint check names out of the source rather than carrying a second copy,
// because a duplicated list drifts invisibly. Same reasoning, one layer up: the
// keys come out of locking.go, the reasons come out of sqlextract, and only the
// Go fixtures live here.
package sqlparse_test

import (
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/sqlextract"
)

// covSeed is the seed literal every fixture below composes on: a SELECT that
// becomes a locking one only once " FOR UPDATE" is appended. That matters for
// the SILENT shapes — the expression walk emits the seed whatever the fold
// does, so a seed that already locked would fire on its own and hide the
// silence this file is measuring.
const covSeed = "SELECT id FROM t WHERE s = 'x'"

// covSrc wraps a body in the standard shape: seed, the composition, a return.
func covSrc(params, body string) string {
	return "package db\n\nfunc load(" + params + ") string {\n" +
		"\tq := \"" + covSeed + "\"\n" + body + "\treturn q\n}\n"
}

// covHelperSrc is covSrc with a helper that writes through a pointer — the
// shape #310 is about.
func covHelperSrc(helper, params, body string) string {
	return "package db\n\n" + helper + "\n\nfunc load(" + params + ") string {\n" +
		"\tq := \"" + covSeed + "\"\n" + body + "\treturn q\n}\n"
}

const covOrderIt = "func orderIt(p *string) { *p += \" ORDER BY id\" }"

// covShapes is one real Go composition per key the block discloses. Nothing
// here says what the verdict should be — that is read out of locking.go, so the
// two cannot be edited into agreement one file at a time.
var covShapes = map[string]string{
	"sprintf-composed": "package db\n\nimport \"fmt\"\n\nfunc load(s string) string {\n" +
		"\treturn fmt.Sprintf(\"SELECT id FROM t WHERE s = '%s' FOR UPDATE\", s)\n}\n",
	"if-branch-append": covSrc("b bool", "\tif b {\n\t\tq += \" FOR UPDATE\"\n\t}\n"),
	"complementary-base": covSrc("a bool",
		"\tif a {\n\t\tq += \" ORDER BY id\"\n\t}\n\tif !a {\n\t\tq += \" LIMIT 10\"\n\t}\n"+
			"\tq += \" FOR UPDATE\"\n"),
	"iife-modellable": covSrc("", "\tfunc() { q += \" FOR UPDATE\" }()\n"),
	"called-closure": covSrc("",
		"\tadd := func() { q += \" ORDER BY id\" }\n\tadd()\n\tq += \" FOR UPDATE\"\n"),
	"called-closure-alias": covSrc("",
		"\tadd := func() { q += \" ORDER BY id\" }\n\tg := add\n\tg()\n\tq += \" FOR UPDATE\"\n"),
	// The spelling the pass CANNOT see, and the reason the two lines above
	// stop where they do: the closure's name is handed to a call instead of
	// being called. `run` invokes it, so the ORDER BY runs on every path and
	// the world emitted here is one no path produces — a false positive that
	// is kept, because `register(add)` where the helper never calls f is the
	// same text to a per-file parse-only pass and there the unordered lock is
	// the REAL value.
	"closure-name-escape": covHelperSrc("func run(f func()) { f() }", "",
		"\tadd := func() { q += \" ORDER BY id\" }\n\trun(add)\n\tq += \" FOR UPDATE\"\n"),
	"forward-goto": covSrc("b bool",
		"\tif b {\n\t\tgoto lock\n\t}\n\tq += \" ORDER BY id\"\nlock:\n\t_ = b\n"+
			"\tq += \" FOR UPDATE\"\n"),
	"backward-goto": covSrc("b bool",
		"top:\n\tq += \" ORDER BY id\"\n\tif b {\n\t\tgoto top\n\t}\n\tq += \" FOR UPDATE\"\n"),
	"deref-write":     covSrc("", "\tp := &q\n\t*p += \" FOR UPDATE\"\n"),
	"alias-read-only": covSrc("", "\tp := &q\n\t_ = len(*p)\n\tq += \" ORDER BY id FOR UPDATE\"\n"),
	"address-escape": covHelperSrc(covOrderIt, "",
		"\torderIt(&q)\n\tq += \" FOR UPDATE\"\n"),
	"escape-under-branch": covHelperSrc(covOrderIt, "b bool",
		"\tif b {\n\t\torderIt(&q)\n\t}\n\tq += \" FOR UPDATE\"\n"),
	"range-clause": covSrc("",
		"\tvar arr [2]string\n\tfor _, q = range arr {\n\t}\n\tq += \" FOR UPDATE\"\n"),
	"range-clause-empty-source": covSrc("m map[string]string",
		"\tfor _, q = range m {\n\t}\n\tq += \" FOR UPDATE\"\n"),
	"unmodelled-write": covSrc("n int",
		"\tswitch n {\n\tcase 1:\n\t\tq = \"x\"\n\t}\n\tq += \" FOR UPDATE\"\n"),
	"strings-builder": "package db\n\nimport \"strings\"\n\nfunc load() string {\n" +
		"\tvar sb strings.Builder\n\tsb.WriteString(\"" + covSeed + "\")\n" +
		"\tsb.WriteString(\" FOR UPDATE\")\n\treturn sb.String()\n}\n",
	// The two the block described only in prose until #311 needed them
	// enumerable. Both carry the ORDER BY inside the literal, which is the
	// direction that FIRES: the rule reports an unordered locking SELECT on
	// code that orders it on every path. With the LOCK inside instead, the
	// same construct emits nothing and the hazard is never analyzed — one
	// construct, two verdicts, which is why neither can be an untrack reason.
	"disqualified-iife": covHelperSrc("func noop() {}", "",
		"\tfunc() {\n\t\tq += \" ORDER BY id\"\n\t\tnoop()\n\t}()\n\tq += \" FOR UPDATE\"\n"),
	"header-literal": covSrc("",
		"\tif func() bool { q += \" ORDER BY id\"; return true }() {\n\t}\n"+
			"\tq += \" FOR UPDATE\"\n"),
}

// covDisclosure is one SHAPE line of the block.
type covDisclosure struct {
	key     string
	verdict string
	issues  []string
	line    int
}

var (
	covShapeRE   = regexp.MustCompile(`(?m)^//\tSHAPE +(\S+) +(FIRES|PASSES|SILENT)((?: +#[0-9]+)*) *$`)
	covIssueRE   = regexp.MustCompile(`#[0-9]+`)
	covVerdictRE = regexp.MustCompile(`\b(FIRES|PASSES|SILENT)\b`)
)

// coverageBlock returns the COVERAGE LIMIT doc comment of CheckFile, verbatim.
// It is delimited by the marker the manual names and by the declaration the
// comment documents, so a block that moves or loses either end fails loudly
// rather than silently shrinking to nothing.
func coverageBlock(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("locking.go")
	if err != nil {
		t.Fatalf("locking.go is this package's own source: %v", err)
	}
	src := string(b)
	start := strings.Index(src, "// COVERAGE LIMIT")
	if start < 0 {
		t.Fatal("no COVERAGE LIMIT marker in locking.go — docs/reference.md sends " +
			"adopters to it by that name")
	}
	end := strings.Index(src[start:], "\nfunc (c *lockingOrder) CheckFile(")
	if end < 0 {
		t.Fatal("the COVERAGE LIMIT block is no longer CheckFile's doc comment — " +
			"whatever it now documents, the manual's pointer is stale")
	}
	return src[start : start+end]
}

// disclosures parses the block's SHAPE lines.
func disclosures(t *testing.T, block string) []covDisclosure {
	t.Helper()
	var out []covDisclosure
	for _, m := range covShapeRE.FindAllStringSubmatchIndex(block, -1) {
		line := 1 + strings.Count(block[:m[0]], "\n")
		out = append(out, covDisclosure{
			key:     block[m[2]:m[3]],
			verdict: block[m[4]:m[5]],
			issues:  covIssueRE.FindAllString(block[m[6]:m[7]], -1),
			line:    line,
		})
	}
	return out
}

// covVerdict runs one composition through the real rule and the real fold.
func covVerdict(t *testing.T, src string) (string, []string) {
	t.Helper()
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	ms := matches(t, c, file("q.go", src))
	cands, _, err := sqlextract.FromGoReassembled("q.go", []byte(src))
	if err != nil {
		t.Fatalf("reassemble: %v", err)
	}
	var worlds []string
	for _, cd := range cands {
		if cd.Text != covSeed && strings.HasPrefix(cd.Text, covSeed) {
			worlds = append(worlds, cd.Text)
		}
	}
	switch {
	case len(ms) > 0:
		return "FIRES", worlds
	case len(worlds) > 0:
		return "PASSES", worlds
	default:
		return "SILENT", worlds
	}
}

// The checkable claim, actually checked.
func TestCoverageLimitDisclosesWhatTheRuleDoes(t *testing.T) {
	block := coverageBlock(t)
	discs := disclosures(t, block)
	if len(discs) < 12 {
		t.Fatalf("only %d SHAPE lines in the COVERAGE LIMIT block — the block "+
			"discloses more compositions than that, so either the machine-readable "+
			"half was dropped or this test's pattern stopped matching it; either "+
			"way the claim is unchecked", len(discs))
	}
	seenVerdict := map[string]bool{}
	for _, d := range discs {
		src, ok := covShapes[d.key]
		if !ok {
			t.Errorf("block line %d discloses shape %q with no composition behind "+
				"it — a state claim survives here only while the behaviour under it "+
				"is pinned", d.line, d.key)
			continue
		}
		got, worlds := covVerdict(t, src)
		seenVerdict[got] = true
		if got != d.verdict {
			t.Errorf("shape %q: the rule says %s, the COVERAGE LIMIT block (line %d) "+
				"says %s — folded worlds %q. An adopter is being told the opposite of "+
				"what the tool does.", d.key, got, d.line, d.verdict, worlds)
		}
	}
	for _, v := range []string{"FIRES", "PASSES", "SILENT"} {
		if !seenVerdict[v] {
			t.Errorf("no shape measured %s — the block distinguishes three verdicts "+
				"and a vocabulary with an unused word is not being checked", v)
		}
	}
}

// Every composition here must be disclosed. A fixture nobody discloses is a
// shape the block is silent about while this file quietly knows the answer.
func TestEveryCoverageFixtureIsDisclosed(t *testing.T) {
	disclosed := map[string]bool{}
	for _, d := range disclosures(t, coverageBlock(t)) {
		disclosed[d.key] = true
	}
	var orphans []string
	for key := range covShapes {
		if !disclosed[key] {
			orphans = append(orphans, key)
		}
	}
	if len(orphans) > 0 {
		sort.Strings(orphans)
		t.Fatalf("%d composition(s) tested here and disclosed nowhere: %s",
			len(orphans), strings.Join(orphans, ", "))
	}
}

// The #313 defect itself: an issue named in the block with nothing behavioural
// behind it. "#73 is OPEN and unmodelled" survived a day because no test could
// tell that sentence from a true one. Now every issue the block cites has to be
// cited by a SHAPE line whose behaviour is measured above.
func TestEveryIssueTheBlockCitesHasAPinnedShape(t *testing.T) {
	block := coverageBlock(t)
	backed := map[string]bool{}
	for _, d := range disclosures(t, block) {
		for _, iss := range d.issues {
			backed[iss] = true
		}
	}
	if len(backed) == 0 {
		t.Fatal("no SHAPE line cites an issue — the assertion below would pass " +
			"vacuously")
	}
	var unbacked []string
	for _, iss := range covIssueRE.FindAllString(block, -1) {
		if !backed[iss] {
			unbacked = append(unbacked, iss)
		}
	}
	if len(unbacked) > 0 {
		sort.Strings(unbacked)
		unbacked = slicesCompact(unbacked)
		t.Fatalf("the COVERAGE LIMIT block cites %s with no SHAPE line behind "+
			"it — that is exactly how it came to say \"#73 is OPEN and unmodelled\" "+
			"seven minutes before #73 landed", strings.Join(unbacked, ", "))
	}
}

// The fold's own list of reasons it declines on (sqlextract.UntrackReasons) is
// the operator-facing half of the same disclosure. Every reason it can report
// must be a shape the block discloses AS SILENT — a reason the rule does not
// actually go silent on would tell an operator a composition went unanalysed
// when it was analysed and passed.
func TestEveryUntrackReasonIsDisclosedAsSilent(t *testing.T) {
	byKey := map[string]covDisclosure{}
	for _, d := range disclosures(t, coverageBlock(t)) {
		byKey[d.key] = d
	}
	reasons := sqlextract.UntrackReasons()
	if len(reasons) == 0 {
		t.Fatal("no untrack reasons — the assertion below would pass vacuously")
	}
	for _, r := range reasons {
		d, ok := byKey[r.Key]
		if !ok {
			t.Errorf("sqlextract can report %q as the reason a query went "+
				"unanalysed, and the COVERAGE LIMIT block does not disclose that "+
				"shape at all", r.Key)
			continue
		}
		if d.verdict != "SILENT" {
			t.Errorf("shape %q is an untrack reason but is disclosed %s — a reason "+
				"the fold reports means no world was folded", r.Key, d.verdict)
		}
		if !containsString(d.issues, r.Issue) {
			t.Errorf("sqlextract cites %s for %q; the block's SHAPE line cites %v",
				r.Issue, r.Key, d.issues)
		}
	}
}

// The verdict vocabulary has to be defined where the disclosure is read, or a
// reader has to guess what SILENT means — and guessing "no findings, so it
// passed" is the misreading the whole block exists to prevent.
func TestCoverageBlockDefinesItsVerdicts(t *testing.T) {
	block := coverageBlock(t)
	for _, v := range []string{"FIRES", "PASSES", "SILENT"} {
		// Once in the legend, at least once in a SHAPE line.
		if strings.Count(block, v) < 2 {
			t.Errorf("the block uses %s without defining it", v)
		}
	}
	if !covVerdictRE.MatchString(block) {
		t.Fatal("no verdict vocabulary in the block at all")
	}
}

func containsString(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func slicesCompact(xs []string) []string {
	out := xs[:0]
	for i, x := range xs {
		if i == 0 || x != xs[i-1] {
			out = append(out, x)
		}
	}
	return out
}

// foldSpecPath is the design spec `fold.go`'s package comment names as this
// mechanism's normative document.
const foldSpecPath = "../../../docs/specs/2026-07-29-sqlextract-assignment-flow-folding-design.md"

var foldSpecTallyRE = regexp.MustCompile(
	`\*\*([0-9]+) FIRES, ([0-9]+) PASSES, ([0-9]+) SILENT\*\*`)

// The spec's own measurement, measured.
//
// §10 carried "Measured at the gate on a seven-shape corpus … 4 findings — 0
// false positives" for two days. No such corpus is in the tree — `grep -rln
// 'seven-shape'` matched that sentence and nothing else — the mechanism it
// claimed to measure had not been built when it was written, and one of its two
// "previously silent hazards firing" is still silent by design. A measurement
// nobody can re-run is a claim wearing a number, and a number is exactly what a
// reader stops questioning.
//
// So the replacement is tied to the run that produces it. The spec states a
// tally; this test computes the same tally from the same nineteen compositions
// and fails if they differ. Add a shape, change a verdict, or revert a fold fix
// in internal/sqlextract, and the spec's number is wrong here rather than wrong
// in somebody's quotation of it six months from now.
func TestFoldSpecMeasurementMatchesTheGate(t *testing.T) {
	b, err := os.ReadFile(foldSpecPath)
	if err != nil {
		t.Fatalf("the fold design spec is the document fold.go points readers at: %v", err)
	}
	m := foldSpecTallyRE.FindStringSubmatch(string(b))
	if m == nil {
		t.Fatalf("the fold spec states no verdict tally this test can re-run — a "+
			"measurement that cannot be reproduced is what #312 was filed about "+
			"(looked for a bold `N FIRES, N PASSES, N SILENT` in %s)", foldSpecPath)
	}
	counted := map[string]int{}
	for _, src := range covShapes {
		got, _ := covVerdict(t, src)
		counted[got]++
	}
	claimed := map[string]string{"FIRES": m[1], "PASSES": m[2], "SILENT": m[3]}
	total := 0
	for _, v := range []string{"FIRES", "PASSES", "SILENT"} {
		if claimed[v] != strconv.Itoa(counted[v]) {
			t.Errorf("the fold spec claims %s %s; the gate measures %d",
				claimed[v], v, counted[v])
		}
		total += counted[v]
	}
	if total != len(covShapes) {
		t.Fatalf("counted %d verdicts over %d compositions", total, len(covShapes))
	}
}

// The block's two halves point at each other. A SHAPE line ends "Item 5 below"
// and the reader is meant to find item 5 in the numbered list under "THE FOLD
// CAN ALSO FIRE ON A QUERY THAT IS ORDERED ON EVERY REAL PATH". Nothing checked
// that the number resolves, and it is the same failure #313 was filed about one
// level down: a pointer into a document is a claim, and a claim nothing
// executes drifts to whatever was true when it was written.
//
// Both directions, because either alone is satisfied by doing nothing. An "Item
// N below" with no item N sends an adopter holding a real finding to a
// paragraph that is not there. An item no SHAPE line points at is a shape
// disclosed in the prose half while the machine-checked half stays silent about
// it — which is how this block came to describe four false positives while the
// rule had five. Scoped to N >= covFirstReferencedItem: items 1 and 2 are the
// pair this pass cannot prove a relationship between, they belong to no single
// composition, and no SHAPE line reaches them.
//
// The count word is the same claim a third time. The sentence introducing the
// list says how many shapes fire on code that is ordered on every path, and a
// list of five under a sentence saying "four" contradicts its own heading.
func TestBlockItemCrossReferencesResolveBothWays(t *testing.T) {
	block := coverageBlock(t)

	items := map[int]bool{}
	for _, m := range covItemRE.FindAllStringSubmatch(block, -1) {
		items[covAtoi(t, m[1])] = true
	}
	if len(items) == 0 {
		t.Fatal("the COVERAGE LIMIT block has no numbered items — every " +
			"assertion below would pass vacuously")
	}

	prose := covProse(block)
	refs := map[int]bool{}
	for _, m := range covItemRefRE.FindAllStringSubmatch(prose, -1) {
		for _, g := range m[1:] {
			if g != "" {
				refs[covAtoi(t, g)] = true
			}
		}
	}
	if len(refs) == 0 {
		t.Fatal(`no "item N below" reference anywhere in the block — the ` +
			"cross-reference assertions below would pass vacuously")
	}

	for _, n := range covSortedKeys(refs) {
		if !items[n] {
			t.Errorf("the block sends a reader to item %d and its numbered list "+
				"stops at %d — a SHAPE line's own explanation of why the shape is "+
				"kept is where that reader is going", n, covMax(items))
		}
	}
	for _, n := range covSortedKeys(items) {
		if n >= covFirstReferencedItem && !refs[n] {
			t.Errorf("numbered item %d is disclosed in the block's prose and no "+
				"SHAPE line points at it — the machine-checked half is then silent "+
				"about a shape the prose half describes", n)
		}
	}

	m := covCountWordRE.FindStringSubmatch(prose)
	if m == nil {
		t.Fatalf("the block introduces its numbered list without saying how many "+
			"shapes are on it (looked for %q in the flattened block) — the count "+
			"is what a reader trusts instead of counting", covCountWordRE)
	}
	claimed, ok := covNumberWords[m[1]]
	if !ok {
		t.Fatalf("the block claims %q disclosed shapes and this test cannot read "+
			"that as a number", m[1])
	}
	if claimed != len(items) {
		t.Errorf("the block says %s (%d) shapes fire on a query ordered on every "+
			"real path and lists %d of them", m[1], claimed, len(items))
	}
}

// covFirstReferencedItem is where the numbered list stops being about a pair of
// values and starts being about one composition a SHAPE line measures.
const covFirstReferencedItem = 3

var (
	covItemRE      = regexp.MustCompile(`(?m)^//[ \t]+([0-9]+)\. `)
	covItemRefRE   = regexp.MustCompile(`(?i)items? ([0-9]+)(?: and ([0-9]+))? below`)
	covCountWordRE = regexp.MustCompile(`in ([a-z]+) disclosed shapes`)
	covNumberWords = map[string]int{
		"one": 1, "two": 2, "three": 3, "four": 4, "five": 5,
		"six": 6, "seven": 7, "eight": 8, "nine": 9, "ten": 10,
	}
)

// covProse flattens the block to running text — comment markers off, lines
// joined — so a sentence that wraps across two comment lines matches as one.
func covProse(block string) string {
	var b strings.Builder
	for _, ln := range strings.Split(block, "\n") {
		b.WriteString(strings.TrimSpace(strings.TrimPrefix(ln, "//")))
		b.WriteByte(' ')
	}
	return b.String()
}

func covAtoi(t *testing.T, s string) int {
	t.Helper()
	n, err := strconv.Atoi(s)
	if err != nil {
		t.Fatalf("the block writes %q where this test expects a number: %v", s, err)
	}
	return n
}

func covSortedKeys(m map[int]bool) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Ints(out)
	return out
}

func covMax(m map[int]bool) int {
	best := 0
	for k := range m {
		if k > best {
			best = k
		}
	}
	return best
}
