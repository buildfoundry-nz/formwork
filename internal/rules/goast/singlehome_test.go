package goast_test

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

const singleHomeType = "go/marked-single-home"

func singleHome(t *testing.T) rules.Checker {
	t.Helper()
	return build(t, singleHomeType, "marker: 'formwork:single-home'")
}

// runAll feeds every file to c and returns the per-file matches plus Finalize.
func runAll(t *testing.T, c rules.Checker, files map[string]string) []rules.Match {
	t.Helper()
	var out []rules.Match
	for path, src := range files {
		ms, err := c.CheckFile(scan.NewMemFile(path, []byte(src)))
		if err != nil {
			t.Fatalf("CheckFile(%s): %v", path, err)
		}
		out = append(out, ms...)
	}
	return append(out, c.(rules.Finalizer).Finalize()...)
}

const home = `package offers

// offerFor builds the one offer a quote carries.
// formwork:single-home
func offerFor(q int) int { return q }
`

func TestSingleHomeAloneIsClean(t *testing.T) {
	if got := runAll(t, singleHome(t), map[string]string{"a/offers.go": home}); len(got) != 0 {
		t.Fatalf("one marked declaration is its own home: %+v", got)
	}
}

func TestSecondDeclarationAnywhereFails(t *testing.T) {
	got := runAll(t, singleHome(t), map[string]string{
		"a/offers.go": home,
		"b/fork.go":   "package fork\n\nfunc offerFor(q int) int { return q + 1 }\n",
	})
	if len(got) != 1 || got[0].Path != "b/fork.go" || got[0].Line != 3 {
		t.Fatalf("the unmarked second declaration is the finding: %+v", got)
	}
	if !strings.Contains(got[0].Message, "a/offers.go:5") {
		t.Fatalf("the finding names the home: %q", got[0].Message)
	}
}

func TestSecondDeclarationInTheSameFileFails(t *testing.T) {
	src := home + "\nfunc offerFor(q int) int { return q }\n"
	if got := runAll(t, singleHome(t), map[string]string{"a/offers.go": src}); len(got) != 1 {
		t.Fatalf("a duplicate beside its home is still a fork: %+v", got)
	}
}

func TestUnmarkedDuplicatesAreNotItsBusiness(t *testing.T) {
	got := runAll(t, singleHome(t), map[string]string{
		"a/x.go": "package a\n\nfunc helper() {}\n",
		"b/y.go": "package b\n\nfunc helper() {}\n",
	})
	if len(got) != 0 {
		t.Fatalf("only a marked name is held to one home: %+v", got)
	}
}

func TestKindsAreDistinctIdentities(t *testing.T) {
	got := runAll(t, singleHome(t), map[string]string{
		"a/offers.go": home,
		"b/y.go":      "package b\n\nvar offerFor = 1\n\ntype T struct{}\n\nfunc (T) offerFor() {}\n",
	})
	if len(got) != 0 {
		t.Fatalf("a var or a method named like a marked func is a different declaration: %+v", got)
	}
}

func TestMarkedMethodIsKeyedOnItsReceiver(t *testing.T) {
	src := "package c\n\ntype Client struct{}\n\n// formwork:single-home\nfunc (c *Client) pump() {}\n"
	got := runAll(t, singleHome(t), map[string]string{
		"c/client.go": src,
		"d/other.go":  "package d\n\ntype Other struct{}\n\nfunc (o Other) pump() {}\n",
		"e/fork.go":   "package e\n\ntype Client struct{}\n\nfunc (c Client) pump() {}\n",
	})
	if len(got) != 1 || got[0].Path != "e/fork.go" {
		t.Fatalf("only a second Client.pump forks it: %+v", got)
	}
}

func TestMarkedConstAndTypeSpecs(t *testing.T) {
	src := "package p\n\n// formwork:single-home\nconst Label = \"auto\"\n\n// formwork:single-home\ntype Palette struct{}\n"
	got := runAll(t, singleHome(t), map[string]string{
		"p/p.go": src,
		"q/q.go": "package q\n\nconst Label = \"auto\"\n\ntype Palette int\n",
	})
	if len(got) != 2 {
		t.Fatalf("a marked const and a marked type each hold one home: %+v", got)
	}
}

func TestGroupedSpecsTakeTheirOwnDoc(t *testing.T) {
	src := "package p\n\nconst (\n\t// formwork:single-home\n\tLabel = \"auto\"\n\tOther = \"x\"\n)\n"
	got := runAll(t, singleHome(t), map[string]string{
		"p/p.go": src,
		"q/q.go": "package q\n\nconst Other = \"x\"\n",
	})
	if len(got) != 0 {
		t.Fatalf("the marker covers the spec it documents, not its siblings: %+v", got)
	}
}

func TestTwoMarkedHomesBothFail(t *testing.T) {
	got := runAll(t, singleHome(t), map[string]string{
		"a/offers.go": home,
		"b/offers.go": strings.Replace(home, "package offers", "package other", 1),
	})
	if len(got) != 2 {
		t.Fatalf("two declarations both claiming the home are both findings: %+v", got)
	}
}

func TestStrayMarkerFails(t *testing.T) {
	src := "package p\n\nfunc f() {\n\t// formwork:single-home\n\tx := 1\n\t_ = x\n}\n"
	got := runAll(t, singleHome(t), map[string]string{"p/p.go": src})
	if len(got) != 1 || got[0].Line != 4 {
		t.Fatalf("a marker on no top-level declaration declares nothing, and says so: %+v", got)
	}
}

func TestSingleHomeRequiresMarker(t *testing.T) {
	if err := buildErr(t, singleHomeType, ""); err == nil {
		t.Fatal("a missing marker must be a config error")
	}
	if err := buildErr(t, singleHomeType, "marker: '  '"); err == nil {
		t.Fatal("a blank marker must be a config error")
	}
}

func TestSingleHomeIsAScopeBoundInvariant(t *testing.T) {
	c := singleHome(t)
	if w, ok := c.(rules.WholeTreeInvariant); !ok || !w.WholeTreeInvariant() {
		t.Fatal("it counts across files, so it judges the whole tree")
	}
	if !rules.IsScopeBound(c) {
		t.Fatal("it reads only the files its scope admits")
	}
}
