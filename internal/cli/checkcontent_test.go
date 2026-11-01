package cli_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/cli"
)

type contentCoverage struct {
	RulesTotal int `json:"rules_total"`
	InScope    int `json:"in_scope"`
	Eligible   int `json:"eligible"`
	Evaluated  int `json:"evaluated"`
	Deferred   int `json:"deferred"`
	OutOfScope int `json:"out_of_scope"`
}

type contentNotEvaluated struct {
	Rule   string `json:"rule"`
	Reason string `json:"reason"`
}

type contentFinding struct {
	Rule     string `json:"rule"`
	Severity string `json:"severity"`
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Message  string `json:"message"`
	Cure     string `json:"cure"`
	Explain  string `json:"explain"`
}

type contentNotScanned struct {
	By     string `json:"by"`
	Glob   string `json:"glob"`
	Reason string `json:"reason"`
}

type contentDocument struct {
	SchemaVersion    int                   `json:"schema_version"`
	EngineVersion    string                `json:"engine_version"`
	CorpusHash       string                `json:"corpus_hash"`
	Path             string                `json:"path"`
	ContentHash      string                `json:"content_hash"`
	Status           string                `json:"status"`
	Coverage         contentCoverage       `json:"coverage"`
	EvaluatedRuleIDs []string              `json:"evaluated_rule_ids"`
	NotEvaluated     []contentNotEvaluated `json:"not_evaluated"`
	Findings         []contentFinding      `json:"findings"`
	FindingsTotal    int                   `json:"findings_total"`
	FindingsOmitted  int                   `json:"findings_omitted"`
	ReplayHandle     string                `json:"replay_handle"`
	ElapsedMS        int64                 `json:"elapsed_ms"`
	NotScanned       *contentNotScanned    `json:"not_scanned"`
}

func contentRepo(t *testing.T, envelope, rules string) string {
	t.Helper()
	root := t.TempDir()
	if envelope == "" {
		envelope = "version: 1\n"
	}
	mustWrite(t, filepath.Join(root, ".formwork", "formwork.yaml"), envelope)
	mustWrite(t, filepath.Join(root, ".formwork", "rules", "rules.yaml"), rules)
	return root
}

func invokeContent(t *testing.T, input string, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := cli.Run(args, strings.NewReader(input), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func decodeContent(t *testing.T, out string) contentDocument {
	t.Helper()
	var doc contentDocument
	dec := json.NewDecoder(strings.NewReader(out))
	if err := dec.Decode(&doc); err != nil {
		t.Fatalf("check-content did not emit one JSON object: %v\n%s", err, out)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		t.Fatalf("check-content emitted trailing JSON/data: err=%v extra=%v", err, extra)
	}
	if doc.EvaluatedRuleIDs == nil || doc.NotEvaluated == nil || doc.Findings == nil {
		t.Fatalf("arrays must be non-nil: %+v", doc)
	}
	return doc
}

func runContent(t *testing.T, input string, args ...string) (int, contentDocument, string) {
	t.Helper()
	code, out, errOut := invokeContent(t, input, args...)
	if out == "" {
		return code, contentDocument{}, errOut
	}
	return code, decodeContent(t, out), errOut
}

func notEvaluatedReasons(doc contentDocument) map[string]string {
	out := map[string]string{}
	for _, entry := range doc.NotEvaluated {
		out[entry.Rule] = entry.Reason
	}
	return out
}

func TestCheckContentUsesStdinNotDiskAndHashesExactBytes(t *testing.T) {
	root := contentRepo(t, "", `rules:
  - id: no-todo
    type: forbidden-pattern
    scope: {include: ['**/*.txt']}
    params: {pattern: TODO}
    cure: resolve the item
  - id: other-only
    type: forbidden-pattern
    scope: {include: ['other/**']}
    params: {pattern: TODO}
`)
	mustWrite(t, filepath.Join(root, "src", "note.txt"), "TODO from disk\n")
	args := []string{"check-content", "-C", root, "--path", "src/note.txt"}
	code, clean, errOut := runContent(t, "clean stdin\n", args...)
	if code != 0 || clean.Status != "clean" {
		t.Fatalf("clean stdin used disk or wrong status: code=%d doc=%+v stderr=%s", code, clean, errOut)
	}
	if !reflect.DeepEqual(clean.EvaluatedRuleIDs, []string{"no-todo"}) {
		t.Fatalf("evaluated = %v, want only no-todo", clean.EvaluatedRuleIDs)
	}
	if _, present := notEvaluatedReasons(clean)["other-only"]; present {
		t.Fatalf("out-of-scope rule must be counted without rule-guidance detail: %+v", clean.NotEvaluated)
	}
	if clean.Coverage.Eligible != 1 || clean.Coverage.Evaluated != 1 || clean.Coverage.OutOfScope != 1 || clean.Coverage.Deferred != 0 {
		t.Fatalf("clean coverage = %+v", clean.Coverage)
	}
	cleanSum := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte("clean stdin\n")))
	if clean.ContentHash != cleanSum {
		t.Fatalf("content hash = %q, want %q", clean.ContentHash, cleanSum)
	}
	if !strings.HasPrefix(clean.CorpusHash, "sha256:") || !strings.HasPrefix(clean.ReplayHandle, "sha256:") {
		t.Fatalf("corpus/replay handles must be SHA-256 fingerprints: %+v", clean)
	}

	code, violation, errOut := runContent(t, "TODO from stdin\n", args...)
	if code != 1 || violation.Status != "findings" {
		t.Fatalf("stdin violation: code=%d doc=%+v stderr=%s", code, violation, errOut)
	}
	if violation.FindingsTotal != 1 || len(violation.Findings) != 1 {
		t.Fatalf("findings = %+v, total=%d", violation.Findings, violation.FindingsTotal)
	}
	got := violation.Findings[0]
	if got.Rule != "no-todo" || got.Severity != "error" || got.Path != "src/note.txt" || got.Line != 1 || got.Cure != "resolve the item" || got.Explain != "formwork explain no-todo" {
		t.Fatalf("finding contract = %+v", got)
	}
	if violation.CorpusHash != clean.CorpusHash {
		t.Fatal("same loaded corpus produced different corpus hashes")
	}
	if violation.ContentHash == clean.ContentHash || violation.ReplayHandle == clean.ReplayHandle {
		t.Fatal("content edit did not change content and replay fingerprints")
	}
	if violation.SchemaVersion != 1 || violation.EngineVersion == "" {
		t.Fatalf("version fields = %+v", violation)
	}
}

func TestCheckContentOmitsOutOfScopeRuleDetails(t *testing.T) {
	t.Run("ordinary result", func(t *testing.T) {
		root := contentRepo(t, "", `rules:
  - id: applicable
    type: forbidden-pattern
    scope: {include: ['**/*.txt']}
    params: {pattern: TODO}
  - id: elsewhere
    type: forbidden-pattern
    scope: {include: ['other/**']}
    params: {pattern: TODO}
`)
		code, doc, errOut := runContent(t, "clean\n", "check-content", "-C", root, "--path", "a.txt")
		if code != 0 || doc.Coverage.OutOfScope != 1 || len(doc.NotEvaluated) != 0 {
			t.Fatalf("out-of-scope rule details must be counted, not enumerated: code=%d doc=%+v stderr=%s", code, doc, errOut)
		}
		if _, present := notEvaluatedReasons(doc)["elsewhere"]; present {
			t.Fatalf("out-of-scope rule was enumerated: %+v", doc.NotEvaluated)
		}
	})
	t.Run("not scanned result", func(t *testing.T) {
		root := contentRepo(t, "version: 1\nscan:\n  ignore:\n    - glob: 'ignored/**'\n      reason: foreign tree\n", `rules:
  - id: applicable
    type: forbidden-pattern
    scope: {include: ['**']}
    params: {pattern: TODO}
  - id: elsewhere
    type: forbidden-pattern
    scope: {include: ['other/**']}
    params: {pattern: TODO}
`)
		code, doc, errOut := runContent(t, "clean\n", "check-content", "-C", root, "--path", "ignored/a.txt")
		if code != 0 || doc.Status != "not_scanned" || len(doc.NotEvaluated) != 0 {
			t.Fatalf("not_scanned must not enumerate the whole corpus: code=%d doc=%+v stderr=%s", code, doc, errOut)
		}
	})
}

func TestCheckContentEmptyInputAndPreprocessor(t *testing.T) {
	t.Run("empty is valid content", func(t *testing.T) {
		root := contentRepo(t, "", `rules:
  - id: no-todo
    type: forbidden-pattern
    scope: {include: ['**']}
    params: {pattern: TODO}
`)
		code, doc, errOut := runContent(t, "", "check-content", "-C", root, "--path", "ghost.txt")
		if code != 0 || doc.Status != "clean" || doc.FindingsTotal != 0 {
			t.Fatalf("empty content: code=%d doc=%+v stderr=%s", code, doc, errOut)
		}
		want := fmt.Sprintf("sha256:%x", sha256.Sum256(nil))
		if doc.ContentHash != want {
			t.Fatalf("empty content hash = %q, want %q", doc.ContentHash, want)
		}
	})
	t.Run("configured preprocessor sees supplied bytes", func(t *testing.T) {
		root := contentRepo(t, "", `rules:
  - id: no-todo-code
    type: forbidden-pattern
    scope: {include: ['**/*.go']}
    preprocess: decomment-go
    params: {pattern: TODO}
`)
		input := "// " + "TODO comment\npackage p\nvar s = \"TODO string\"\n"
		code, doc, errOut := runContent(t, input, "check-content", "-C", root, "--path", "a.go")
		if code != 1 || doc.FindingsTotal != 1 || doc.Findings[0].Line != 3 {
			t.Fatalf("preprocessor result: code=%d doc=%+v stderr=%s", code, doc, errOut)
		}
	})
}

func TestCheckContentEngineErrorAndWarnExit(t *testing.T) {
	t.Run("analyzer error is exit 2", func(t *testing.T) {
		root := contentRepo(t, "", `rules:
  - id: go-budget
    type: go/func-line-budget
    scope: {include: ['**/*.go']}
    params: {max_lines: 1}
`)
		code, out, errOut := invokeContent(t, "package p\nfunc (\n", "check-content", "-C", root, "--path", "bad.go")
		if code != 2 || out != "" || !strings.Contains(errOut, "go-budget") {
			t.Fatalf("engine error: code=%d out=%q stderr=%q", code, out, errOut)
		}
	})
	t.Run("warn findings report but exit 0", func(t *testing.T) {
		root := contentRepo(t, "", `rules:
  - id: warn-todo
    type: forbidden-pattern
    severity: warn
    scope: {include: ['**']}
    params: {pattern: TODO}
`)
		code, doc, errOut := runContent(t, "TODO\n", "check-content", "-C", root, "--path", "a.txt")
		if code != 0 || doc.Status != "findings" || doc.FindingsTotal != 1 {
			t.Fatalf("warn result: code=%d doc=%+v stderr=%s", code, doc, errOut)
		}
	})
}

func TestCheckContentBoundsOutputButNotVerdictAndAllFindingsReplays(t *testing.T) {
	root := contentRepo(t, "", `rules:
  - id: a-warn
    type: forbidden-pattern
    severity: warn
    scope: {include: ['**']}
    params: {pattern: WARN}
  - id: z-error
    type: forbidden-pattern
    scope: {include: ['**']}
    params: {pattern: ERROR}
`)
	input := "WARN one\nWARN two\nWARN three\nERROR four\n"
	args := []string{"check-content", "-C", root, "--path", "a.txt"}
	code, bounded, errOut := runContent(t, input, args...)
	if code != 1 {
		t.Fatalf("omitted error finding must still determine exit: code=%d stderr=%s", code, errOut)
	}
	if len(bounded.Findings) != 3 || bounded.FindingsTotal != 4 || bounded.FindingsOmitted != 1 {
		t.Fatalf("bounded counts = len %d total %d omitted %d", len(bounded.Findings), bounded.FindingsTotal, bounded.FindingsOmitted)
	}
	for _, f := range bounded.Findings {
		if f.Severity != "warn" {
			t.Fatalf("test setup no longer places error past the bound: %+v", bounded.Findings)
		}
	}
	allArgs := append(append([]string{}, args...), "--all-findings")
	code, all, errOut := runContent(t, input, allArgs...)
	if code != 1 || len(all.Findings) != 4 || all.FindingsTotal != 4 || all.FindingsOmitted != 0 {
		t.Fatalf("--all-findings: code=%d doc=%+v stderr=%s", code, all, errOut)
	}
	if all.CorpusHash != bounded.CorpusHash || all.ContentHash != bounded.ContentHash || all.ReplayHandle != bounded.ReplayHandle {
		t.Fatal("output bound changed replay identity")
	}
}

func TestCheckContentDefersTreeDependentFamilies(t *testing.T) {
	root := contentRepo(t, "", `rules:
  - id: a-plain
    type: forbidden-pattern
    scope: {include: ['**']}
    params: {pattern: NEVER}
  - id: min-files
    type: forbidden-pattern
    scope: {include: ['**'], min_files: 1}
    params: {pattern: TODO}
  - id: not-declaring
    type: forbidden-pattern
    scope:
      include: ['**']
      not_declaring: [{marker: 'HOME MARKER', holders: 1}]
    params: {pattern: TODO}
  - id: generated-only
    type: forbidden-pattern
    scope: {include: ['**'], generated: true}
    params: {pattern: TODO}
  - id: whole-tree
    type: required-pattern
    scope: {include: ['**']}
    params: {pattern: REQUIRED, mode: exists}
  - id: census-fast
    type: forbidden-pattern
    scope: {include: ['**']}
    params:
      census:
        cmd: [sh, -c, 'echo census-must-not-run; exit 1']
  - id: credential
    type: forbidden-pattern
    scope: {include: ['**']}
    params:
      pattern: TODO
      require_absent:
        - {credential: '^const Owner = "TODO"', holders: 1}
  - id: command
    type: command
    scope: {include: ['**']}
    params: {cmd: [sh, -c, 'echo command-must-not-run; exit 1']}
  - id: git-diff
    type: git-diff
    scope: {include: ['**']}
    params: {range: HEAD..HEAD, forbid_added: TODO}
  - id: tree-reader
    type: doc-path-exists
    scope: {include: ['**']}
    params: {pattern: '(src/[A-Za-z0-9_.]+)'}
  - id: out-of-scope
    type: forbidden-pattern
    scope: {include: ['other/**']}
    params: {pattern: TODO}
`)
	code, doc, errOut := runContent(t, "TODO\n", "check-content", "-C", root, "--path", "a.go")
	if code != 0 {
		t.Fatalf("deferred findings must not leak into verdict: code=%d doc=%+v stderr=%s", code, doc, errOut)
	}
	if !reflect.DeepEqual(doc.EvaluatedRuleIDs, []string{"a-plain"}) {
		t.Fatalf("evaluated = %v, want only plain file-local rule; doc=%+v", doc.EvaluatedRuleIDs, doc)
	}
	want := map[string]string{
		"min-files": "scope_min_files", "not-declaring": "scope_not_declaring",
		"generated-only": "generated_only_scope", "whole-tree": "finalizer",
		"census-fast": "finalizer", "credential": "finalizer", "command": "finalizer",
		"git-diff": "finalizer", "tree-reader": "finalizer",
	}
	got := notEvaluatedReasons(doc)
	for id, reason := range want {
		if got[id] != reason {
			t.Errorf("%s reason = %q, want %q", id, got[id], reason)
		}
	}
	if _, present := got["out-of-scope"]; present {
		t.Errorf("out-of-scope rule must not carry guidance detail: %+v", doc.NotEvaluated)
	}
	if doc.Coverage.Deferred != 9 || doc.Coverage.OutOfScope != 1 {
		t.Fatalf("deferred/out-of-scope coverage = %+v", doc.Coverage)
	}
	if strings.Contains(errOut, "must-not-run") || doc.FindingsTotal != 0 {
		t.Fatalf("a deferred checker ran: doc=%+v stderr=%s", doc, errOut)
	}
}

func TestCheckContentGeneratedScopes(t *testing.T) {
	root := contentRepo(t, "version: 1\ngenerated:\n  - marker: 'Code generated by test'\n    proven_by: [generated-only]\n", `rules:
  - id: ordinary
    type: forbidden-pattern
    scope: {include: ['**']}
    params: {pattern: TODO}
  - id: generated-only
    type: forbidden-pattern
    scope: {include: ['**'], generated: true}
    params: {pattern: TODO}
`)
	code, doc, errOut := runContent(t, "// Code generated by test. DO NOT EDIT.\nTODO\n", "check-content", "-C", root, "--path", "a.go")
	if code != 0 || doc.Status != "no_eligible_rules" {
		t.Fatalf("generated scopes: code=%d doc=%+v stderr=%s", code, doc, errOut)
	}
	got := notEvaluatedReasons(doc)
	if got["ordinary"] != "generated_excluded" || got["generated-only"] != "generated_only_scope" {
		t.Fatalf("generated reasons = %v", got)
	}
}

func TestCheckContentSQLPathOptIn(t *testing.T) {
	rules := `rules:
  - id: sql-parses
    type: sql/parses
    scope: {include: ['**']}
    params: {}
  - id: sql-predicate
    type: sql/statement-predicate
    scope: {include: ['**']}
    params: {table: '.*', forbid: ['DROP']}
  - id: sql-lock-order
    type: sql/locking-select-order
    scope: {include: ['**']}
    params: {order_requires_unique_key: false}
  - id: sql-lock-target
    type: sql/locking-target
    scope: {include: ['**']}
    params: {table: projects, strength: [update]}
`
	root := contentRepo(t, "", rules)
	code, doc, errOut := runContent(t, "SELECT 1;\n", "check-content", "-C", root, "--path", "db/q.sql")
	if code != 0 || doc.Coverage.Evaluated != 4 {
		t.Fatalf(".sql must evaluate all SQL analyzers: code=%d doc=%+v stderr=%s", code, doc, errOut)
	}
	for _, path := range []string{"db/q.SQL", "db/q.Sql", "db/q.sQL"} {
		code, doc, errOut = runContent(t, "SELECT 1;\n", "check-content", "-C", root, "--path", path)
		if code != 0 || doc.Coverage.Evaluated != 4 {
			t.Errorf("%s must use the analyzers' case-insensitive SQL classification: code=%d doc=%+v stderr=%s", path, code, doc, errOut)
		}
	}
	code, doc, errOut = runContent(t, "package p\n", "check-content", "-C", root, "--path", "db/q.go")
	if code != 0 || doc.Status != "no_eligible_rules" || doc.Coverage.Evaluated != 0 || doc.Coverage.Deferred != 4 {
		t.Fatalf("SQL on .go must defer: code=%d doc=%+v stderr=%s", code, doc, errOut)
	}
	for id, reason := range notEvaluatedReasons(doc) {
		if reason != "content_only_not_supported" {
			t.Errorf("%s on .go reason = %q", id, reason)
		}
	}
}

func TestCheckContentSafeGoAndDartAnalyzers(t *testing.T) {
	t.Run("go", func(t *testing.T) {
		root := contentRepo(t, "", `rules:
  - id: go-budget
    type: go/func-line-budget
    scope: {include: ['**/*.go']}
    params: {max_lines: 0, funcs: Bad}
`)
		input := "package p\nfunc Bad() {\n\tprintln(\"x\")\n}\n"
		code, doc, errOut := runContent(t, input, "check-content", "-C", root, "--path", "a.go")
		if code != 1 || doc.FindingsTotal != 1 {
			t.Fatalf("safe Go analyzer: code=%d doc=%+v stderr=%s", code, doc, errOut)
		}
	})
	t.Run("dart", func(t *testing.T) {
		root := contentRepo(t, "", `rules:
  - id: dart-await
    type: dart/ref-after-await
    scope: {include: ['**/*.dart']}
    params: {method: build}
`)
		input := "Widget build() {\n  await work();\n  ref.read(value);\n}\n"
		code, doc, errOut := runContent(t, input, "check-content", "-C", root, "--path", "a.dart")
		if code != 1 || doc.FindingsTotal != 1 {
			t.Fatalf("safe Dart analyzer: code=%d doc=%+v stderr=%s", code, doc, errOut)
		}
	})
}

func TestCheckContentAdditionalEligibleFamilies(t *testing.T) {
	t.Run("path size binary and ordering", func(t *testing.T) {
		root := contentRepo(t, "", `rules:
  - id: binary-cap
    type: binary-content
    scope: {include: ['**']}
    params: {max_bytes: 1}
  - id: file-name
    type: file-naming
    scope: {include: ['**']}
    params: {forbid_ext: ['.txt']}
  - id: file-size
    type: file-size
    scope: {include: ['**']}
    params: {cap: 1}
  - id: ordering
    type: ordering
    scope: {include: ['**']}
    params: {before: A, after: B}
`)
		code, doc, errOut := runContent(t, "B\nA\n", "check-content", "-C", root, "--path", "a.txt")
		if code != 1 || doc.Coverage.Evaluated != 4 || doc.FindingsTotal != 4 {
			t.Fatalf("filesystem/ordering families: code=%d doc=%+v stderr=%s", code, doc, errOut)
		}
	})
	t.Run("remaining file-local Go analyzers", func(t *testing.T) {
		root := contentRepo(t, "", `rules:
  - id: derives
    type: go/expected-derives-from-actual
    scope: {include: ['**']}
    params: {reader: '^os\.ReadFile$', compare: '^bytes\.Equal$', declare: round-trip}
  - id: confined
    type: go/call-confined-to-func-name
    scope: {include: ['**']}
    params: {symbol: 'sql\.Query', allowed_func: '^Allowed$'}
  - id: guard
    type: go/guard-precedes-call
    scope: {include: ['**']}
    params: {guard: Check, sink: Write}
`)
		code, doc, errOut := runContent(t, "package p\nfunc Allowed() {}\n", "check-content", "-C", root, "--path", "a.go")
		if code != 0 || doc.Coverage.Evaluated != 3 {
			t.Fatalf("remaining Go analyzers: code=%d doc=%+v stderr=%s", code, doc, errOut)
		}
	})
	t.Run("remaining file-local Dart analyzers", func(t *testing.T) {
		root := contentRepo(t, "", `rules:
  - id: numeric
    type: dart/numeric-field-validated
    scope: {include: ['**']}
    params: {numeric_value: number}
  - id: gate-reads
    type: dart/gate-reads-are-listened
    scope: {include: ['**']}
    params:
      builders: [AnimatedBuilder]
      listen_args: [animation]
      builder_arg: builder
      read_suffixes: ['.text']
`)
		code, doc, errOut := runContent(t, "class A {}\n", "check-content", "-C", root, "--path", "a.dart")
		if code != 0 || doc.Coverage.Evaluated != 2 {
			t.Fatalf("remaining Dart analyzers: code=%d doc=%+v stderr=%s", code, doc, errOut)
		}
	})
}

func TestCheckContentZeroEligibleIsNotCleanCoverage(t *testing.T) {
	root := contentRepo(t, "", `rules:
  - id: elsewhere
    type: forbidden-pattern
    scope: {include: ['other/**']}
    params: {pattern: TODO}
`)
	code, doc, errOut := runContent(t, "TODO\n", "check-content", "-C", root, "--path", "a.txt")
	if code != 0 || doc.Status != "no_eligible_rules" || doc.Coverage.Eligible != 0 || doc.Coverage.Evaluated != 0 {
		t.Fatalf("zero eligible: code=%d doc=%+v stderr=%s", code, doc, errOut)
	}
	if doc.FindingsTotal != 0 || len(doc.EvaluatedRuleIDs) != 0 || doc.Coverage.OutOfScope != 1 {
		t.Fatalf("zero-eligible coverage = %+v", doc)
	}
}

func TestCheckContentPathReachability(t *testing.T) {
	t.Run("nonexistent intended file evaluates", func(t *testing.T) {
		root := contentRepo(t, "", `rules:
  - id: no-todo
    type: forbidden-pattern
    scope: {include: ['**/*.txt']}
    params: {pattern: TODO}
`)
		code, doc, errOut := runContent(t, "clean\n", "check-content", "-C", root, "--path", "new/ghost.txt")
		if code != 0 || doc.Path != "new/ghost.txt" || doc.Coverage.Evaluated != 1 {
			t.Fatalf("nonexistent path: code=%d doc=%+v stderr=%s", code, doc, errOut)
		}
	})
	t.Run("ignored path is not scanned", func(t *testing.T) {
		root := contentRepo(t, "version: 1\nscan:\n  ignore:\n    - glob: 'ignored/**'\n      reason: foreign tree\n", `rules:
  - id: no-todo
    type: forbidden-pattern
    scope: {include: ['**']}
    params: {pattern: TODO}
`)
		code, doc, errOut := runContent(t, "TODO\n", "check-content", "-C", root, "--path", "ignored/a.txt")
		if code != 0 || doc.Status != "not_scanned" || doc.NotScanned == nil || doc.NotScanned.By != "scan.ignore" || doc.Coverage.Evaluated != 0 {
			t.Fatalf("ignored path: code=%d doc=%+v stderr=%s", code, doc, errOut)
		}
	})
	t.Run("existing and future gitignored paths are not scanned", func(t *testing.T) {
		root := contentRepo(t, "version: 1\nscan:\n  gitignore:\n    reason: git refuses these paths\n", `rules:
  - id: no-todo
    type: forbidden-pattern
    scope: {include: ['**']}
    params: {pattern: TODO}
`)
		gitInit(t, root)
		mustWrite(t, filepath.Join(root, ".gitignore"), "ignored.txt\nghost.txt\n")
		mustWrite(t, filepath.Join(root, "ignored.txt"), "disk\n")
		for _, path := range []string{"ignored.txt", "ghost.txt"} {
			code, doc, errOut := runContent(t, "TODO\n", "check-content", "-C", root, "--path", path)
			if code != 0 || doc.Status != "not_scanned" || doc.NotScanned == nil || doc.NotScanned.By != "scan.gitignore" {
				t.Fatalf("gitignored path %s: code=%d doc=%+v stderr=%s", path, code, doc, errOut)
			}
		}
	})
	t.Run("nonregular path is not scanned", func(t *testing.T) {
		root := contentRepo(t, "", `rules:
  - id: no-todo
    type: forbidden-pattern
    scope: {include: ['**']}
    params: {pattern: TODO}
`)
		mustWrite(t, filepath.Join(root, "target.txt"), "disk\n")
		if err := os.Symlink("target.txt", filepath.Join(root, "link.txt")); err != nil {
			t.Fatal(err)
		}
		code, doc, errOut := runContent(t, "TODO\n", "check-content", "-C", root, "--path", "link.txt")
		if code != 0 || doc.Status != "not_scanned" || doc.NotScanned == nil || doc.NotScanned.By != "non-regular" {
			t.Fatalf("nonregular path: code=%d doc=%+v stderr=%s", code, doc, errOut)
		}
	})
}

type failingContentReader struct{}

func (failingContentReader) Read([]byte) (int, error) { return 0, fmt.Errorf("stdin exploded") }

func TestCheckContentStdinReadErrorExits2(t *testing.T) {
	root := contentRepo(t, "", "rules: []\n")
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"check-content", "-C", root, "--path", "a.txt"}, failingContentReader{}, &stdout, &stderr)
	if code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "stdin exploded") {
		t.Fatalf("stdin error: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

type failingContentWriter struct{}

func (failingContentWriter) Write([]byte) (int, error) { return 0, fmt.Errorf("stdout ENOSPC") }

func TestCheckContentStdoutWriteErrorExits2(t *testing.T) {
	root := contentRepo(t, "version: 1\nscan:\n  ignore:\n    - glob: 'ignored/**'\n      reason: foreign tree\n", `rules:
  - id: no-todo
    type: forbidden-pattern
    scope: {include: ['**']}
    params: {pattern: TODO}
`)
	for _, tc := range []struct {
		name string
		path string
	}{
		{name: "clean result", path: "a.txt"},
		{name: "not_scanned result", path: "ignored/a.txt"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stderr bytes.Buffer
			code := cli.Run([]string{"check-content", "-C", root, "--path", tc.path}, strings.NewReader("clean\n"), failingContentWriter{}, &stderr)
			if code != 2 || !strings.Contains(stderr.String(), "writing json") || !strings.Contains(stderr.String(), "stdout ENOSPC") {
				t.Fatalf("stdout error: code=%d stderr=%q, want exit 2 naming the write failure", code, stderr.String())
			}
		})
	}
}

func TestCheckContentReplayHandleChangesWithReachability(t *testing.T) {
	root := contentRepo(t, "", `rules:
  - id: no-todo
    type: forbidden-pattern
    scope: {include: ['**']}
    params: {pattern: TODO}
`)
	mustWrite(t, filepath.Join(root, "target.txt"), "disk\n")
	mustWrite(t, filepath.Join(root, "a.txt"), "disk\n")
	args := []string{"check-content", "-C", root, "--path", "a.txt"}
	code, findings, errOut := runContent(t, "TODO\n", args...)
	if code != 1 || findings.Status != "findings" {
		t.Fatalf("initial findings result: code=%d doc=%+v stderr=%s", code, findings, errOut)
	}
	if err := os.Remove(filepath.Join(root, "a.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target.txt", filepath.Join(root, "a.txt")); err != nil {
		t.Fatal(err)
	}
	code, hidden, errOut := runContent(t, "TODO\n", args...)
	if code != 0 || hidden.Status != "not_scanned" {
		t.Fatalf("symlink result: code=%d doc=%+v stderr=%s", code, hidden, errOut)
	}
	if hidden.ReplayHandle == findings.ReplayHandle {
		t.Fatal("findings and not_scanned states share a replay handle")
	}
	code, repeated, errOut := runContent(t, "TODO\n", args...)
	if code != 0 || repeated.Status != "not_scanned" || repeated.ReplayHandle != hidden.ReplayHandle {
		t.Fatalf("identical not_scanned state is unstable: code=%d first=%q second=%q stderr=%s", code, hidden.ReplayHandle, repeated.ReplayHandle, errOut)
	}
}

func TestCheckContentReplayHandleIncludesGitIgnoreRule(t *testing.T) {
	root := contentRepo(t, "version: 1\nscan:\n  gitignore:\n    reason: git refuses these paths\n", `rules:
  - id: no-todo
    type: forbidden-pattern
    scope: {include: ['**']}
    params: {pattern: TODO}
`)
	gitInit(t, root)
	mustWrite(t, filepath.Join(root, ".gitignore"), "ghost.txt\n")
	args := []string{"check-content", "-C", root, "--path", "ghost.txt"}
	_, first, _ := runContent(t, "clean\n", args...)
	mustWrite(t, filepath.Join(root, ".gitignore"), "gh*.txt\n")
	_, second, _ := runContent(t, "clean\n", args...)
	if first.Status != "not_scanned" || second.Status != "not_scanned" || first.CorpusHash != second.CorpusHash {
		t.Fatalf("test setup changed more than the gitignore rule: first=%+v second=%+v", first, second)
	}
	if first.ReplayHandle == second.ReplayHandle {
		t.Fatal("different deciding gitignore rules share a replay handle")
	}
}

func TestCheckContentRejectsBadInvocation(t *testing.T) {
	root := contentRepo(t, "", "rules: []\n")
	for _, args := range [][]string{
		{"check-content", "-C", root},
		{"check-content", "-C", root, "--path", "a.txt", "extra"},
		{"check-content", "-C", root, "--path", "a.txt", "--bogus"},
		{"check-content", "-C", root, "--path", "."},
	} {
		code, out, _ := invokeContent(t, "", args...)
		if code != 2 || out != "" {
			t.Errorf("args %v: code=%d out=%q, want exit 2 and no report", args, code, out)
		}
	}
}
