package fixturetest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseGoRunShapeFingerprintsBuildArgsWithDashC(t *testing.T) {
	// go -C scripts/dev/filesizecaps run ../check-file-size-guardrails.go
	// must fingerprint the file go build actually reads — not only chDir.
	shape, ok := parseGoRunShape([]string{
		"go", "-C", "scripts/dev/filesizecaps", "run", "../check-file-size-guardrails.go", "--report",
	})
	if !ok {
		t.Fatal("expected go-run shape")
	}
	if shape.chDir != "scripts/dev/filesizecaps" {
		t.Fatalf("chDir=%q", shape.chDir)
	}
	if len(shape.buildArgs) != 1 || shape.buildArgs[0] != "../check-file-size-guardrails.go" {
		t.Fatalf("buildArgs=%v", shape.buildArgs)
	}
	if len(shape.progArgs) != 1 || shape.progArgs[0] != "--report" {
		t.Fatalf("progArgs=%v", shape.progArgs)
	}
	want := "scripts/dev/check-file-size-guardrails.go"
	found := false
	for _, p := range shape.treePaths {
		if p == want {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("treePaths %v must include %q (buildArg resolved under -C), not only chDir", shape.treePaths, want)
	}
	for _, p := range shape.treePaths {
		if p == "scripts/dev/filesizecaps" {
			t.Fatalf("treePaths must not be the whole -C directory when buildArgs name a narrower input; got %v", shape.treePaths)
		}
	}
}

func TestParseGoRunShapeDashCPackageNotWholeModule(t *testing.T) {
	shape, ok := parseGoRunShape([]string{
		"go", "-C", "api-factory", "run", "./services/core-api/cmd/detailcatalogseed",
	})
	if !ok {
		t.Fatal("expected go-run shape")
	}
	want := "api-factory/services/core-api/cmd/detailcatalogseed"
	found := false
	for _, p := range shape.treePaths {
		if p == want {
			found = true
		}
		if p == "api-factory" {
			t.Fatalf("treePaths must not hash the entire -C module root; got %v", shape.treePaths)
		}
	}
	if !found {
		t.Fatalf("treePaths %v must include package %q", shape.treePaths, want)
	}
}

func TestLocalReplaceDirsSkipsModulePathTargets(t *testing.T) {
	dir := t.TempDir()
	mod := "module example.com/det\n\ngo 1.22\n\n" +
		"replace example.com/lib => ../lib\n" +
		"replace example.com/remote => github.com/x/y v1.2.3\n" +
		"replace example.com/abs => /tmp/elsewhere\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := localReplaceDirs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want only the local ../lib replace, got %v", got)
	}
	want := filepath.Clean(filepath.Join(dir, "../lib"))
	if got[0] != want {
		t.Fatalf("got %q want %q", got[0], want)
	}
	for _, p := range got {
		if strings.Contains(p, "github.com") {
			t.Fatalf("module-path replace must not be treated as a filesystem dir: %q", p)
		}
	}
}
