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

func TestTreeDigestReplaceCycleErrors(t *testing.T) {
	// A⇄B local replaces must not stack-overflow; hash error → cold fallback.
	arm := t.TempDir()
	a := filepath.Join(arm, "a")
	b := filepath.Join(arm, "b")
	for _, dir := range []string{a, b} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(a, "go.mod"), []byte("module example.com/a\n\ngo 1.22\n\nreplace example.com/b => ../b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b, "go.mod"), []byte("module example.com/b\n\ngo 1.22\n\nreplace example.com/a => ../a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b, "b.go"), []byte("package b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := treeDigest(arm, goRunShape{treePaths: []string{"a"}})
	if err == nil || !strings.Contains(err.Error(), "replace cycle") {
		t.Fatalf("want replace cycle error, got %v", err)
	}
}

func TestTreeDigestReplaceEscapingArmErrors(t *testing.T) {
	arm := t.TempDir()
	pkg := filepath.Join(arm, "pkg")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	// replace => ../../outside escapes the arm root.
	mod := "module example.com/pkg\n\ngo 1.22\n\nreplace example.com/shared => ../../outside\n"
	if err := os.WriteFile(filepath.Join(pkg, "go.mod"), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := treeDigest(arm, goRunShape{treePaths: []string{"pkg"}})
	if err == nil || !strings.Contains(err.Error(), "escapes arm root") {
		t.Fatalf("want escapes-arm error, got %v", err)
	}
}

func TestTreeDigestSkipsGoToolIgnoredDirs(t *testing.T) {
	arm := t.TempDir()
	pkg := filepath.Join(arm, "pkg")
	if err := os.MkdirAll(filepath.Join(pkg, "testdata"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "testdata", "extra.go"), []byte("package testdata\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d1, err := treeDigest(arm, goRunShape{treePaths: []string{"pkg"}})
	if err != nil {
		t.Fatal(err)
	}
	// Changing testdata must not change the digest (go build ignores it).
	if err := os.WriteFile(filepath.Join(pkg, "testdata", "extra.go"), []byte("package testdata\nconst X = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d2, err := treeDigest(arm, goRunShape{treePaths: []string{"pkg"}})
	if err != nil {
		t.Fatal(err)
	}
	if d1 != d2 {
		t.Fatalf("testdata/.go changes must not affect digest")
	}
}
