// scope_format_test.go — `formwork scope -format human|json` (#330).
//
// WHY THIS EXISTS. docs/reference.md's Introspection section opens "Every one
// takes `-format human|json`" over a block that lists scope. It did not:
// `formwork scope -format json` printed "flag provided but not defined:
// -format" and exited 2 — which, under this repo's own exit-code contract,
// reads to a CI step as "the engine never ran". scope is the introspection
// command most likely to be consumed by a machine (it exists to route CI
// lanes), and it was the one with no machine format.
//
// The assumed-classification key is the load-bearing part. runScope's
// fail-closed arms announce on STDERR that a class was assumed rather than
// computed, because "an assumed classification that looks identical to a
// computed one is how this went unnoticed at exit 0" (scope.go). A JSON
// surface that dropped that distinction would reintroduce the same defect for
// the consumer least able to notice it: json.Unmarshal does not read stderr.
package cli_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// scopeJSON is the wire shape a consumer decodes. Declared in the test rather
// than shared with the production DTO on purpose: a test that unmarshals into
// the very struct it is checking cannot catch a renamed json tag.
type scopeJSON struct {
	Class     string          `json:"class"`
	Languages map[string]bool `json:"languages"`
	Assumed   string          `json:"assumed"`
}

func decodeScopeJSON(t *testing.T, out string) scopeJSON {
	t.Helper()
	var got scopeJSON
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("stdout must be a single JSON document: %v\nstdout:\n%s", err, out)
	}
	if strings.Contains(out, "class=") {
		t.Fatalf("-format json must not emit the human key=value lines:\n%s", out)
	}
	return got
}

func TestScopeEmitsJSONForNamedPaths(t *testing.T) {
	root := scopeOperandRepo(t)
	code, out, errOut := runCLI(t, "scope", "-C", root, "-format", "json", "docs/a.md")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	got := decodeScopeJSON(t, out)
	if got.Class != "docs" {
		t.Errorf("class = %q, want docs\n%s", got.Class, out)
	}
	// Every declared language is present, not just the changed ones: a
	// consumer keying off `languages["dart"]` must be able to tell false from
	// absent.
	for _, l := range []string{"go", "dart"} {
		if v, ok := got.Languages[l]; !ok || v {
			t.Errorf("languages[%q] = %v, present=%v; want false and present\n%s", l, v, ok, out)
		}
	}
	if got.Assumed != "" {
		t.Errorf("a named path is computed, never assumed; assumed = %q", got.Assumed)
	}
}

func TestScopeEmitsJSONForTheChangeset(t *testing.T) {
	root := scopeOperandRepo(t) // internal/x.go is staged
	code, out, errOut := runCLI(t, "scope", "-C", root, "-format", "json", "--staged")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	got := decodeScopeJSON(t, out)
	if got.Class != "runtime" || !got.Languages["go"] || got.Languages["dart"] {
		t.Errorf("staged internal/x.go should be runtime + go only:\n%s", out)
	}
	if got.Assumed != "" {
		t.Errorf("git answered and named a path — nothing was assumed; assumed = %q", got.Assumed)
	}
}

// The two fail-closed arms are the reason the key exists. Both are exit 0 with
// a confident-looking class, and stderr is the only thing that says the class
// was assumed — which a JSON consumer is not reading.
func TestScopeJSONMarksAnAssumedClassification(t *testing.T) {
	t.Run("empty changeset", func(t *testing.T) {
		root := t.TempDir()
		mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"),
			"version: 1\nscope:\n  docs: ['**/*.md']\n  languages:\n    go: ['**/*.go']\n")
		gitInit(t, root)
		gitRun(t, root, "add", ".formwork")
		gitRun(t, root, "commit", "-q", "-m", "init")

		code, out, _ := runCLI(t, "scope", "-C", root, "-format", "json", "--staged")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, out)
		}
		got := decodeScopeJSON(t, out)
		if got.Class != "runtime" || !got.Languages["go"] {
			t.Errorf("an empty changeset assumes the strongest class:\n%s", out)
		}
		if got.Assumed == "" {
			t.Errorf("the JSON surface must say the class was assumed, not computed:\n%s", out)
		}
	})
	t.Run("git error", func(t *testing.T) {
		root := t.TempDir() // config, deliberately not a git repo
		mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"),
			"version: 1\nscope:\n  docs: ['**/*.md']\n  languages:\n    go: ['**/*.go']\n")

		code, out, _ := runCLI(t, "scope", "-C", root, "-format", "json")
		if code != 0 {
			t.Fatalf("exit = %d, want 0\n%s", code, out)
		}
		got := decodeScopeJSON(t, out)
		if got.Class != "runtime" || !got.Languages["go"] {
			t.Errorf("a git failure assumes the strongest class:\n%s", out)
		}
		if got.Assumed == "" {
			t.Errorf("the JSON surface must say the class was assumed, not computed:\n%s", out)
		}
	})
}

// The Introspection preface's other promise: "An unknown id, kind, format ...
// is exit 2". scope joins the three commands already keeping it, through the
// same introspectFormat validator, so the wording cannot drift apart.
func TestScopeRefusesAnUnknownFormat(t *testing.T) {
	root := scopeOperandRepo(t)
	code, out, errOut := runCLI(t, "scope", "-C", root, "-format", "yaml")
	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if strings.Contains(out, "class") {
		t.Errorf("a refused format must print no classification:\n%s", out)
	}
	if !strings.Contains(errOut, "unknown format") {
		t.Errorf("stderr must name the format, got %q", errOut)
	}
}

// Adding the flag must not have moved the default. `formwork scope` with no
// -format is the form every existing hook and doc uses.
func TestScopeHumanRemainsTheDefaultFormat(t *testing.T) {
	root := scopeOperandRepo(t)
	_, implicit, _ := runCLI(t, "scope", "-C", root, "docs/a.md")
	code, explicit, errOut := runCLI(t, "scope", "-C", root, "-format", "human", "docs/a.md")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", code, explicit, errOut)
	}
	if implicit != explicit {
		t.Errorf("-format human must be the default:\nimplicit:\n%s\nexplicit:\n%s", implicit, explicit)
	}
	for _, want := range []string{"class=docs", "go_changed=false", "dart_changed=false"} {
		if !strings.Contains(implicit, want) {
			t.Errorf("the human contract is unchanged; want %q in:\n%s", want, implicit)
		}
	}
}
