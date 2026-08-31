package goast

import (
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/scan"
	"gopkg.in/yaml.v3"
)

// RED tests for #3 — a conformance test whose EXPECTED value is derived from
// the very artifact it is checking compares that artifact to itself. It can
// only ever falsify the round trip — ordering, dedup, formatting — never
// agreement with whatever the artifact is supposed to track.
//
// THE TRIGGER IS THE UNDECLARED SELF-COMPARISON, NOT THE SHAPE. A round-trip
// normalisation check is legitimate and worth pinning. The first design flagged
// the shape itself and a prototype proved that wrong: after two real members
// were corrected — headers, names and failure messages narrowed to say
// render-normalisation — the analyzer still fired on both. Correct behaviour,
// wrong rule. So a test that compares an artifact to a re-render of itself must
// SAY SO, and the declaration is what clears it.

const derivesParams = `reader: '^os\.ReadFile$'
loader: '^(load|read)[A-Z]\w*$'
compare: '^(bytes\.Equal|reflect\.DeepEqual|cmp\.Diff)$'
declare: 'self-comparison:'
`

func derivesRule(t *testing.T) *expectedDerives {
	t.Helper()
	var n yaml.Node
	if err := yaml.Unmarshal([]byte(derivesParams), &n); err != nil {
		t.Fatal(err)
	}
	c, err := newExpectedDerives(n.Content[0])
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	return c.(*expectedDerives)
}

func derivesCheck(t *testing.T, src string) []string {
	t.Helper()
	ms, err := derivesRule(t).CheckFile(scan.NewMemFile("subject_test.go", []byte(src)))
	if err != nil {
		t.Fatalf("CheckFile: %v", err)
	}
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.Message)
	}
	return out
}

// TestDerivesFiresOnTheRealMember is the shape both live members had: the file
// is read once, loaded again through a loader pointed at the same artifact, and
// the two are compared. The header claimed agreement with a writer it never
// consulted.
func TestDerivesFiresOnTheRealMember(t *testing.T) {
	t.Parallel()
	const src = `package p

func TestRegistryMatchesWriter(t *testing.T) {
	root := repoRoot(t)
	abs := filepath.Join(root, rel)
	inv, present, err := loadLiveIncludeInventory(root)
	_ = present
	_ = err
	got, err := os.ReadFile(abs)
	if !bytes.Equal(got, renderIncludeRegistry(inv)) {
		t.Fatal("mismatch")
	}
}
`
	if ms := derivesCheck(t, src); len(ms) != 1 {
		t.Fatalf("want 1 finding on an undeclared self-comparison, got %d: %v", len(ms), ms)
	}
}

// TestDerivesSilentOnAGoldenFileControl is the false-positive direction, and it
// is the dangerous one: the correct form and the defective form are the same
// TEXT. A golden test reading a committed file and comparing it to freshly
// rendered output is the RIGHT shape; what separates them is whether the
// expected side traces back to the same artifact.
func TestDerivesSilentOnAGoldenFileControl(t *testing.T) {
	t.Parallel()
	const src = `package p

func TestGolden(t *testing.T) {
	root := repoRoot(t)
	want, err := os.ReadFile(filepath.Join(root, "testdata", "golden.txt"))
	_ = err
	got := render(buildModelFromScratch())
	if !bytes.Equal(got, want) {
		t.Fatal("mismatch")
	}
}
`
	if ms := derivesCheck(t, src); len(ms) != 0 {
		t.Fatalf("fired on a legitimate golden-file test: %v", ms)
	}
}

// TestDerivesSilentOnADifferentArtifact keeps the artifact identity honest: a
// loader reading something else is not a self-comparison.
func TestDerivesSilentOnADifferentArtifact(t *testing.T) {
	t.Parallel()
	const src = `package p

func TestTwoArtifacts(t *testing.T) {
	root := repoRoot(t)
	other := otherRoot(t)
	got, err := os.ReadFile(filepath.Join(root, rel))
	_ = err
	inv, _, _ := loadLiveIncludeInventory(other)
	if !bytes.Equal(got, renderIncludeRegistry(inv)) {
		t.Fatal("mismatch")
	}
}
`
	if ms := derivesCheck(t, src); len(ms) != 0 {
		t.Fatalf("fired on reads of two different artifacts: %v", ms)
	}
}

// TestDerivesClearedByTheDeclaration is the design correction. Self-comparison
// is not the defect; an UNDECLARED self-comparison carrying a claim it does not
// test is. Declaring it makes the claim explicit and reviewable.
func TestDerivesClearedByTheDeclaration(t *testing.T) {
	t.Parallel()
	const src = `package p

// self-comparison: this is a render-normalisation round trip. It pins that the
// generated file stays diffable and merge-friendly; it does NOT check agreement
// with the writer.
func TestRegistryIsRenderNormalised(t *testing.T) {
	root := repoRoot(t)
	abs := filepath.Join(root, rel)
	inv, _, _ := loadLiveIncludeInventory(root)
	got, err := os.ReadFile(abs)
	_ = err
	if !bytes.Equal(got, renderIncludeRegistry(inv)) {
		t.Fatal("not render-normalised")
	}
}
`
	if ms := derivesCheck(t, src); len(ms) != 0 {
		t.Fatalf("a declared round trip was still reported: %v", ms)
	}
}

// TestDerivesTreatsAFunCallAsNoOrigin is implementation note 1. Counting a
// call's Fun as a data origin makes every filepath.Join share an origin with
// every other and collapses the whole analysis into "everything is related".
func TestDerivesTreatsAFunCallAsNoOrigin(t *testing.T) {
	t.Parallel()
	const src = `package p

func TestUnrelated(t *testing.T) {
	a := filepath.Join(rootA, "x")
	b := filepath.Join(rootB, "y")
	got, _ := os.ReadFile(a)
	inv, _, _ := loadThing(b)
	if !bytes.Equal(got, render(inv)) {
		t.Fatal("mismatch")
	}
}
`
	if ms := derivesCheck(t, src); len(ms) != 0 {
		t.Fatalf("two filepath.Join calls were treated as a shared origin: %v", ms)
	}
}

// TestDerivesExcludesFunctionParameters is implementation note 2. *testing.T
// reaches every helper in the body; counting a parameter as an origin makes two
// unrelated roots share one, and then every comparison in every test reads as a
// self-comparison. The exclusion is derived from the signature, never a name
// list.
func TestDerivesExcludesFunctionParameters(t *testing.T) {
	t.Parallel()
	const src = `package p

func checkBoth(t *testing.T, a string, b string) {
	got, _ := os.ReadFile(a)
	inv, _, _ := loadThing(b)
	if !bytes.Equal(got, render(inv)) {
		t.Fatal("mismatch")
	}
}
`
	if ms := derivesCheck(t, src); len(ms) != 0 {
		t.Fatalf("parameters were counted as origins, so unrelated roots collapsed: %v", ms)
	}
}
