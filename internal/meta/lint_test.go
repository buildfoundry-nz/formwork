package meta_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/meta"
	_ "github.com/buildfoundry-nz/formwork/internal/preprocess"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/binarycontent"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/command"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/dartscan"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/docpathexists"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/filenaming"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/gitdiff"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/goast"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/pairconsistency"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/pattern"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/patterncount"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/setrelation"
)

func TestLintExemptsExternalToolRulesFromFixturesAndScope(t *testing.T) {
	// A command and a git-diff rule have no fixtures and (for command) a scope
	// matching nothing here, yet lint stays clean: they are tracked by the
	// escape-hatch enumeration, not by fixtures/empty-scope.
	failed, out := lint(t, map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml": lintRule +
			"  - id: gofmt-clean\n    type: command\n    scope: {include: ['**/*.go']}\n    params: {cmd: [true]}\n    fixture_exempt: \"external tool; a fixture tree cannot drive it\"\n" +
			"  - id: no-new-panic\n    type: git-diff\n    scope: {include: ['**']}\n    params: {range: A..B, forbid_added: panic}\n    fixture_exempt: \"external tool; a fixture tree cannot drive it\"\n",
		".formwork/fixtures/no-banana/fire-1/f.txt": "banana want: no-banana\n",
		".formwork/fixtures/no-banana/pass-1/f.txt": "clean\n",
		"notes.txt": "in scope\n",
	})
	if failed != 0 {
		t.Fatalf("failed=%d\n%s", failed, out)
	}
	for _, want := range []string{
		"[fixture-coverage] OK",
		"[empty-scope] OK",
		"gofmt-clean: command rule (external tool, heavy — NO firing proof: no fixtures)",
		"no-new-panic: git-diff rule (external tool, heavy — NO firing proof: no fixtures)",
		"formwork lint: 5/5 checks passed",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestLintSkipsFixtureCoverageForLibraryRules(t *testing.T) {
	failed, out := lint(t, map[string]string{
		".formwork/formwork.yaml":                   "version: 1\nlibrary: [generic]\n",
		".formwork/rules/r.yaml":                    lintRule,
		".formwork/fixtures/no-banana/fire-1/f.txt": "banana want: no-banana\n",
		".formwork/fixtures/no-banana/pass-1/f.txt": "clean\n",
		"notes.txt": "in scope\n",
	})
	if failed != 0 {
		t.Fatalf("failed=%d\n%s", failed, out)
	}
	if !strings.Contains(out, "[fixture-coverage] OK") {
		t.Fatalf("library rules demanded local fixtures:\n%s", out)
	}
}

func writeRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func lint(t *testing.T, files map[string]string) (int, string) {
	t.Helper()
	root := writeRepo(t, files)
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	// A `go test` binary is never built with the release ldflags stamp, so
	// its trusted gate version is always the "dev" sentinel — exactly what
	// cli.devOptOutActive() would see for an actual dev binary. Reproduce its
	// computation here (env var parses truthy; gate version == "dev" always
	// holds in this binary) rather than hardcoding true/false, so tests that
	// set FORMWORK_ALLOW_DEV still see the real end-to-end behaviour.
	devOptOutActive, _ := strconv.ParseBool(os.Getenv("FORMWORK_ALLOW_DEV"))
	failed, err := meta.Lint(cfg, root, &sb, devOptOutActive, false)
	if err != nil {
		t.Fatal(err)
	}
	return failed, sb.String()
}

const lintRule = "rules:\n  - id: no-banana\n    type: forbidden-pattern\n    scope: {include: ['**/*.txt']}\n    params: {pattern: banana}\n"

func TestLintCleanRepo(t *testing.T) {
	failed, out := lint(t, map[string]string{
		".formwork/formwork.yaml":                   "version: 1\n",
		".formwork/rules/r.yaml":                    lintRule,
		".formwork/fixtures/no-banana/fire-1/f.txt": "banana want: no-banana\n",
		".formwork/fixtures/no-banana/pass-1/f.txt": "clean\n",
		"notes.txt": "in scope\n",
	})
	if failed != 0 {
		t.Fatalf("failed=%d\n%s", failed, out)
	}
	for _, want := range []string{
		"[fixture-coverage] OK",
		"[empty-scope] OK",
		"[exemption-hygiene] OK",
		"escape hatches: none",
		"formwork lint: 5/5 checks passed",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestLintFlagsMissingFixturesAndEmptyScope(t *testing.T) {
	failed, out := lint(t, map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml":  lintRule,
		// no fixtures at all, and no .txt file anywhere in the repo
		"README.md": "not a txt file\n",
	})
	if failed != 2 {
		t.Fatalf("failed=%d\n%s", failed, out)
	}
	for _, want := range []string{
		"[fixture-coverage] FAIL — 2 problem(s)",
		"no-banana: no fire fixture (want .formwork/fixtures/no-banana/fire-*/)",
		"no-banana: no pass fixture (want .formwork/fixtures/no-banana/pass-*/)",
		"[empty-scope] FAIL — 1 problem(s)",
		"no-banana: scope matches no files in this repo",
		"formwork lint: 3/5 checks passed",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestLintFlagsMissingPassFixtureOnly(t *testing.T) {
	failed, out := lint(t, map[string]string{
		".formwork/formwork.yaml":                   "version: 1\n",
		".formwork/rules/r.yaml":                    lintRule,
		".formwork/fixtures/no-banana/fire-1/f.txt": "banana want: no-banana\n",
		"notes.txt": "in scope\n",
	})
	if failed != 1 || !strings.Contains(out, "no-banana: no pass fixture") {
		t.Fatalf("failed=%d\n%s", failed, out)
	}
	if strings.Contains(out, "no fire fixture") {
		t.Fatalf("fire fixture wrongly flagged:\n%s", out)
	}
}

const exemptLintRule = "rules:\n  - id: no-banana\n    type: forbidden-pattern\n" +
	"    scope: {include: ['**/*.txt']}\n    params: {pattern: banana}\n" +
	"    except: {marker: true, allowlist: allowlists/legacy.txt}\n"

func TestLintFlagsStaleAllowlistEntries(t *testing.T) {
	failed, out := lint(t, map[string]string{
		".formwork/formwork.yaml":                   "version: 1\n",
		".formwork/rules/r.yaml":                    exemptLintRule,
		".formwork/allowlists/legacy.txt":           "# legacy\ngone.txt\nclean.txt\nhit.txt\n",
		".formwork/fixtures/no-banana/fire-1/f.txt": "banana want: no-banana\n",
		".formwork/fixtures/no-banana/pass-1/f.txt": "clean\n",
		"clean.txt": "no fruit here\n",
		"hit.txt":   "banana\n",
	})
	if failed != 1 {
		t.Fatalf("failed=%d\n%s", failed, out)
	}
	for _, want := range []string{
		"[exemption-hygiene] FAIL — 2 problem(s)",
		"no-banana: allowlist allowlists/legacy.txt:2: gone.txt does not exist",
		"no-banana: allowlist allowlists/legacy.txt:3: clean.txt no longer trips the rule (stale)",
		"formwork lint: 4/5 checks passed",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "hit.txt no longer trips") {
		t.Fatalf("live allowlist entry flagged stale:\n%s", out)
	}
}

func TestLintFlagsReasonlessMarker(t *testing.T) {
	failed, out := lint(t, map[string]string{
		".formwork/formwork.yaml":                   "version: 1\n",
		".formwork/rules/r.yaml":                    exemptLintRule,
		".formwork/allowlists/legacy.txt":           "hit.txt\n",
		".formwork/fixtures/no-banana/fire-1/f.txt": "banana want: no-banana\n",
		".formwork/fixtures/no-banana/pass-1/f.txt": "clean\n",
		"hit.txt":  "banana\n",
		"lazy.txt": "banana // formwork:allow no-banana\n",
	})
	if failed != 1 {
		t.Fatalf("failed=%d\n%s", failed, out)
	}
	for _, want := range []string{
		"[exemption-hygiene] FAIL — 1 problem(s)",
		"no-banana: lazy.txt:1: formwork:allow marker missing a reason",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestLintFlagsReasonlessMarkerCommentCloser(t *testing.T) {
	// A block-comment closer directly after the id is not a real reason
	// (marker package finding B1); lint must flag this now, where the old
	// end-anchored regex missed it.
	failed, out := lint(t, map[string]string{
		".formwork/formwork.yaml":                   "version: 1\n",
		".formwork/rules/r.yaml":                    exemptLintRule,
		".formwork/allowlists/legacy.txt":           "hit.txt\n",
		".formwork/fixtures/no-banana/fire-1/f.txt": "banana want: no-banana\n",
		".formwork/fixtures/no-banana/pass-1/f.txt": "clean\n",
		"hit.txt":  "banana\n",
		"lazy.txt": "banana /* formwork:allow no-banana */\n",
	})
	if failed != 1 {
		t.Fatalf("failed=%d\n%s", failed, out)
	}
	for _, want := range []string{
		"[exemption-hygiene] FAIL — 1 problem(s)",
		"no-banana: lazy.txt:1: formwork:allow marker missing a reason",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestLintFlagsReasonlessMarkerCRLF(t *testing.T) {
	// A CRLF tail with no real reason after the id must also be flagged
	// (marker package finding B1); the old end-anchored regex missed lines
	// ending in '\r'.
	failed, out := lint(t, map[string]string{
		".formwork/formwork.yaml":                   "version: 1\n",
		".formwork/rules/r.yaml":                    exemptLintRule,
		".formwork/allowlists/legacy.txt":           "hit.txt\n",
		".formwork/fixtures/no-banana/fire-1/f.txt": "banana want: no-banana\n",
		".formwork/fixtures/no-banana/pass-1/f.txt": "clean\n",
		"hit.txt":  "banana\n",
		"lazy.txt": "banana // formwork:allow no-banana\r\n",
	})
	if failed != 1 {
		t.Fatalf("failed=%d\n%s", failed, out)
	}
	for _, want := range []string{
		"[exemption-hygiene] FAIL — 1 problem(s)",
		"no-banana: lazy.txt:1: formwork:allow marker missing a reason",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestLintIgnoresReasonlessMarkerWhenNotOptedIn(t *testing.T) {
	// A rule without `except: {marker: true}` hasn't opted markers in at all:
	// a reasonless `formwork:allow` for it is misleading noise, not hygiene
	// debt (adding a reason wouldn't make it exempt anything). It belongs to
	// the deferred stale-marker bucket, not this check.
	failed, out := lint(t, map[string]string{
		".formwork/formwork.yaml":                   "version: 1\n",
		".formwork/rules/r.yaml":                    lintRule,
		".formwork/fixtures/no-banana/fire-1/f.txt": "banana want: no-banana\n",
		".formwork/fixtures/no-banana/pass-1/f.txt": "clean\n",
		"lazy.txt": "banana // formwork:allow no-banana\n",
	})
	if failed != 0 {
		t.Fatalf("failed=%d\n%s", failed, out)
	}
	if strings.Contains(out, "formwork:allow marker missing a reason") {
		t.Fatalf("reasonless marker flagged for rule without marker: true:\n%s", out)
	}
}

func TestLintEnumeratesFinalizerOnlyMarkerAnnotation(t *testing.T) {
	// required-pattern in exists mode only ever reports via Finalize (no
	// per-file findings), so marker suppression can never apply to it
	// (phase-3a carryover, finding B2). The enumeration must say so.
	failed, out := lint(t, map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml": "rules:\n  - id: has-license\n    type: required-pattern\n" +
			"    scope: {include: ['**/*.txt']}\n    params: {pattern: LICENSE, mode: exists}\n" +
			"    except: {marker: true}\n",
		".formwork/fixtures/has-license/fire-1/f.txt": "no license here\n",
		".formwork/fixtures/has-license/pass-1/f.txt": "LICENSE\n",
		"notes.txt": "in scope, has LICENSE\n",
	})
	if failed != 0 {
		t.Fatalf("failed=%d\n%s", failed, out)
	}
	if !strings.Contains(out, "  has-license: marker enabled (finalizer findings: allowlist-only)") {
		t.Fatalf("missing finalizer-marker annotation in:\n%s", out)
	}
}

func TestLintEnumeratesEscapeHatches(t *testing.T) {
	failed, out := lint(t, map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml": "rules:\n  - id: no-banana\n    type: forbidden-pattern\n" +
			"    scope: {include: ['**/*.txt']}\n    params: {pattern: banana}\n" +
			"    except: {paths: ['vendor/**'], marker: true, allowlist: allowlists/legacy.txt}\n",
		".formwork/allowlists/legacy.txt":           "hit.txt\n",
		".formwork/fixtures/no-banana/fire-1/f.txt": "banana want: no-banana\n",
		".formwork/fixtures/no-banana/pass-1/f.txt": "clean\n",
		"hit.txt": "banana\n",
	})
	if failed != 0 {
		t.Fatalf("failed=%d\n%s", failed, out)
	}
	for _, want := range []string{
		"escape hatches:",
		// The count is part of the assertion on purpose. Asserting only the
		// declared glob is satisfied by the declaration-only line this channel
		// used to print (#138) — the new line CONTAINS the old one, so the
		// weaker form could not tell the two apart. `vendor/**` matches nothing
		// in this repo, so 0 is the honest answer and a fossil entry is exactly
		// what it should read as.
		"  no-banana: except.paths: vendor/**: 0 file(s) removed",
		"  no-banana: marker enabled",
		"  no-banana: allowlist allowlists/legacy.txt (1 entries)",
		"formwork lint: 5/5 checks passed",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

// TestLintEnumeratesActiveEngineBackstopOptOut: the engine-version backstop
// is itself an escape hatch when FORMWORK_ALLOW_DEV is actively disabling it
// (a constraint is configured AND the env var parses as true) — spec §9's
// "nothing is silently excluded" requires it show up here too.
func TestLintEnumeratesActiveEngineBackstopOptOut(t *testing.T) {
	t.Setenv("FORMWORK_ALLOW_DEV", "1")
	failed, out := lint(t, map[string]string{
		".formwork/formwork.yaml":                   "version: 1\nengine: \">=0.2.0\"\n",
		".formwork/rules/r.yaml":                    lintRule,
		".formwork/fixtures/no-banana/fire-1/f.txt": "banana want: no-banana\n",
		".formwork/fixtures/no-banana/pass-1/f.txt": "clean\n",
		"notes.txt": "in scope\n",
	})
	if failed != 0 {
		t.Fatalf("failed=%d\n%s", failed, out)
	}
	if !strings.Contains(out, "engine-version backstop: DISABLED via FORMWORK_ALLOW_DEV=1") {
		t.Fatalf("missing active engine-backstop opt-out line in:\n%s", out)
	}
}

// TestLintOmitsEngineBackstopLineWhenInertOrUnset covers both cases where the
// line must NOT appear: no engine constraint configured (opt-out is inert),
// and a constraint configured but the env var unset (backstop still
// enforcing).
func TestLintOmitsEngineBackstopLineWhenInertOrUnset(t *testing.T) {
	cases := []struct {
		name     string
		root     string
		allowDev string
	}{
		{"no engine constraint, opt-out set", "version: 1\n", "1"},
		// c.allowDev == "" here sets FORMWORK_ALLOW_DEV to the empty string
		// explicitly (via t.Setenv below), rather than leaving it untouched —
		// os.Getenv treats an explicit "" and a truly-unset var identically,
		// so this reproduces "opt-out unset" while still being hermetic
		// against whatever the ambient environment happens to have exported
		// (this subtest previously called no Setenv at all and inherited the
		// ambient value, which failed for any developer with
		// FORMWORK_ALLOW_DEV set — the same class of defect as the
		// internal/cli non-hermetic tests fixed last round).
		{"engine constraint, opt-out unset", "version: 1\nengine: \">=0.2.0\"\n", ""},
		{"engine constraint, opt-out falsy", "version: 1\nengine: \">=0.2.0\"\n", "0"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("FORMWORK_ALLOW_DEV", c.allowDev)
			failed, out := lint(t, map[string]string{
				".formwork/formwork.yaml":                   c.root,
				".formwork/rules/r.yaml":                    lintRule,
				".formwork/fixtures/no-banana/fire-1/f.txt": "banana want: no-banana\n",
				".formwork/fixtures/no-banana/pass-1/f.txt": "clean\n",
				"notes.txt": "in scope\n",
			})
			if failed != 0 {
				t.Fatalf("failed=%d\n%s", failed, out)
			}
			if strings.Contains(out, "engine-version backstop: DISABLED") {
				t.Fatalf("engine-backstop opt-out line should not appear:\n%s", out)
			}
		})
	}
}

// twoTaggedRules: a go-tagged rule and a misc-tagged rule, each with a
// non-overlapping scope so empty-scope can pass when both a .go and a .txt
// file exist in the repo.
const twoTaggedRules = "rules:\n" +
	"  - id: go-rule\n    type: forbidden-pattern\n    scope: {include: ['**/*.go']}\n    params: {pattern: TODO}\n    tags: [go]\n" +
	"  - id: orphan\n    type: forbidden-pattern\n    scope: {include: ['**/*.txt']}\n    params: {pattern: TODO}\n    tags: [misc]\n"

// lanedFixtures gives both rules fire/pass fixtures and both scopes a matching
// file, so fixture-coverage and empty-scope pass — isolating lane-reachability.
func lanedFixtures() map[string]string {
	return map[string]string{
		".formwork/rules/r.yaml":                 twoTaggedRules,
		".formwork/fixtures/go-rule/fire-1/f.go": "TODO want: go-rule\n",
		".formwork/fixtures/go-rule/pass-1/f.go": "clean\n",
		".formwork/fixtures/orphan/fire-1/f.txt": "TODO want: orphan\n",
		".formwork/fixtures/orphan/pass-1/f.txt": "clean\n",
		"a.go":                                   "package main\n",
		"notes.txt":                              "clean\n",
	}
}

func withRoot(files map[string]string, root string) map[string]string {
	files[".formwork/formwork.yaml"] = root
	return files
}

func TestLintLaneReachabilityFlagsUnreachableRule(t *testing.T) {
	// A ci lane selecting only `go`-tagged rules leaves `orphan` (tags: misc)
	// unreachable — no ci lane runs it — so lane-reachability fails.
	failed, out := lint(t, withRoot(lanedFixtures(),
		"version: 1\nlanes:\n  ci:\n    tags: [go]\n    ci: true\n"))
	if failed != 1 {
		t.Fatalf("failed=%d\n%s", failed, out)
	}
	for _, want := range []string{
		"[lane-reachability] FAIL — 1 problem(s)",
		"orphan: not selected by any ci lane",
		"formwork lint: 6/7 checks passed",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "go-rule: not selected") {
		t.Fatalf("go-rule wrongly flagged unreachable:\n%s", out)
	}
}

func TestLintLaneReachabilityRequiresCILane(t *testing.T) {
	// A lane that selects a rule but does not run in CI (ci: false) does not
	// make it reachable — every rule must be in at least one ci lane.
	failed, out := lint(t, withRoot(lanedFixtures(),
		"version: 1\nlanes:\n  pre-commit:\n    all: true\n"))
	if failed != 1 {
		t.Fatalf("failed=%d\n%s", failed, out)
	}
	for _, want := range []string{
		"[lane-reachability] FAIL — 2 problem(s)",
		"go-rule: not selected by any ci lane",
		"orphan: not selected by any ci lane",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestLintLaneReachabilityPasses(t *testing.T) {
	// An all/ci lane selects every rule — lane-reachability passes 4/4.
	failed, out := lint(t, withRoot(lanedFixtures(),
		"version: 1\nlanes:\n  ci:\n    all: true\n    ci: true\n"))
	if failed != 0 {
		t.Fatalf("failed=%d\n%s", failed, out)
	}
	for _, want := range []string{
		"[lane-reachability] OK",
		"formwork lint: 7/7 checks passed",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestLintEnumeratesScopeExclude(t *testing.T) {
	// Every scope.exclude entry must appear in the escape-hatch census — it is
	// an exemption channel just like except.paths. Live excludes (that match at
	// least one scanned file) need no comment; they only need to be visible.
	failed, out := lint(t, map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml": "rules:\n  - id: no-banana\n    type: forbidden-pattern\n" +
			"    scope:\n      include: ['**/*.txt']\n      exclude: ['vendor/**']\n" +
			"    params: {pattern: banana}\n",
		".formwork/fixtures/no-banana/fire-1/f.txt": "banana want: no-banana\n",
		".formwork/fixtures/no-banana/pass-1/f.txt": "clean\n",
		"notes.txt":          "in scope\n",
		"vendor/ignored.txt": "banana\n", // makes the exclude live so hygiene stays clean
	})
	if failed != 0 {
		t.Fatalf("failed=%d\n%s", failed, out)
	}
	if !strings.Contains(out, "  no-banana: scope.exclude: vendor/**") {
		t.Fatalf("missing scope.exclude census line in:\n%s", out)
	}
}

func TestLintFlagsDeadUncommentedScopeExclude(t *testing.T) {
	// A dead exclude (matches zero scanned files) with no YAML justification
	// comment is exemption rot — the cheap hole in a rule that every instrument
	// used to miss. Fixture proves the hygiene arm reports it.
	failed, out := lint(t, map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml": "rules:\n  - id: no-banana\n    type: forbidden-pattern\n" +
			"    scope:\n      include: ['**/*.txt']\n      exclude:\n        - 'never-existed/**'\n" +
			"    params: {pattern: banana}\n",
		".formwork/fixtures/no-banana/fire-1/f.txt": "banana want: no-banana\n",
		".formwork/fixtures/no-banana/pass-1/f.txt": "clean\n",
		"notes.txt": "in scope\n",
	})
	if failed != 1 {
		t.Fatalf("failed=%d\n%s", failed, out)
	}
	for _, want := range []string{
		"[exemption-hygiene] FAIL",
		`no-banana: scope.exclude "never-existed/**" matches no files and has no justification comment`,
		"  no-banana: scope.exclude: never-existed/**", // still enumerated
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestLintAcceptsDeadScopeExcludeWithComment(t *testing.T) {
	// Preventative excludes of trees the walker may not see (vendor/build/
	// generated) are legitimate when the YAML entry carries a justification
	// comment. Hygiene must not ban them — only the uncommented dead ones.
	failed, out := lint(t, map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml": "rules:\n  - id: no-banana\n    type: forbidden-pattern\n" +
			"    scope:\n      include: ['**/*.txt']\n      exclude:\n" +
			"        - 'vendor/**' # preventative: vendored trees are not always present on disk\n" +
			"    params: {pattern: banana}\n",
		".formwork/fixtures/no-banana/fire-1/f.txt": "banana want: no-banana\n",
		".formwork/fixtures/no-banana/pass-1/f.txt": "clean\n",
		"notes.txt": "in scope\n",
	})
	if failed != 0 {
		t.Fatalf("failed=%d\n%s", failed, out)
	}
	if strings.Contains(out, "matches no files and has no justification comment") {
		t.Fatalf("commented dead exclude wrongly failed hygiene:\n%s", out)
	}
	if !strings.Contains(out, "  no-banana: scope.exclude: vendor/**") {
		t.Fatalf("missing scope.exclude census line in:\n%s", out)
	}
}

func TestLintEnumeratesScanIgnoreWithMatchCounts(t *testing.T) {
	_, out := lintTracked(t, map[string]string{
		".formwork/formwork.yaml":                   "version: 1\nscan:\n  ignore:\n    - glob: 'vendor/**'\n      reason: vendored source\n    - glob: 'zz-nothing/**'\n      reason: reserved for future noise\n",
		".formwork/rules/r.yaml":                    lintRule,
		".formwork/fixtures/no-banana/fire-1/f.txt": "banana want: no-banana\n",
		".formwork/fixtures/no-banana/pass-1/f.txt": "clean\n",
		"vendor/a.txt":                              "x\n",
		"vendor/sub/b.txt":                          "x\n",
		"notes.txt":                                 "in scope\n",
	}, ".formwork", "notes.txt") // vendor/* stays on disk untracked — the feature working, not a bypass
	// vendor/** matches the dir `vendor` itself (doublestar zero-segment **),
	// so the walk prunes it without descending: 1 dir, 0 files — files under
	// a pruned dir are deliberately never counted. zz-nothing/** matches
	// nothing: the census must expose the dead glob, not hide it.
	for _, want := range []string{
		"engine: never scans directories named .formwork, .git (any depth)",
		"scan.ignore: vendor/** — 1 dirs pruned (subtrees not scanned), 0 files ignored (vendored source)",
		"scan.ignore: zz-nothing/** — 0 matches (reserved for future noise)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("lint output missing %q\n%s", want, out)
		}
	}
}

func TestLintCensusUnaffectedWhenScanIgnoreUnset(t *testing.T) {
	// The clean-repo contract "escape hatches: none" (TestLintCleanRepo) must
	// survive: no scan lines when the key is unconfigured.
	_, out := lint(t, map[string]string{
		".formwork/formwork.yaml":                   "version: 1\n",
		".formwork/rules/r.yaml":                    lintRule,
		".formwork/fixtures/no-banana/fire-1/f.txt": "banana want: no-banana\n",
		".formwork/fixtures/no-banana/pass-1/f.txt": "clean\n",
		"notes.txt": "in scope\n",
	})
	if strings.Contains(out, "scan.ignore") {
		t.Fatalf("scan.ignore lines leaked into an unconfigured census:\n%s", out)
	}
	// #56 inverted the other half. The engine skip set is now reported on every
	// run — it is the one exemption no rule can declare, so the census is the
	// only place it is auditable — but ABOVE the escape-hatch block, so
	// "escape hatches: none" still means "this repo declares no exemptions".
	if !strings.Contains(out, "never scans directories named") {
		t.Fatalf("the engine skip set must be reported even with scan.ignore unset:\n%s", out)
	}
	if !strings.Contains(out, "escape hatches: none") {
		t.Fatalf("clean-repo census contract broken:\n%s", out)
	}
}

func TestLintAllowlistEntryUnderScanIgnoreSaysSoNotDoesNotExist(t *testing.T) {
	failed, out := lintTracked(t, map[string]string{
		".formwork/formwork.yaml":                   "version: 1\nscan:\n  ignore:\n    - glob: 'vendor/**'\n      reason: vendored\n",
		".formwork/rules/r.yaml":                    exemptLintRule,
		".formwork/allowlists/legacy.txt":           "vendor/gen.txt\nhit.txt\n",
		".formwork/fixtures/no-banana/fire-1/f.txt": "banana want: no-banana\n",
		".formwork/fixtures/no-banana/pass-1/f.txt": "clean\n",
		"vendor/gen.txt":                            "banana\n", // exists on disk, hidden by the ignore
		"hit.txt":                                   "banana\n", // keeps the allowlist otherwise live
	}, ".formwork", "hit.txt") // vendor/gen.txt stays untracked: this test is about the hygiene diagnosis, not #90
	if failed == 0 {
		t.Fatalf("want hygiene failure: allowlisted path can never fire\n%s", out)
	}
	if !strings.Contains(out, "vendor/gen.txt hidden by scan.ignore (vendor/**)") {
		t.Fatalf("want truthful ignore diagnosis, got:\n%s", out)
	}
	if strings.Contains(out, "vendor/gen.txt does not exist") {
		t.Fatalf("misleading does-not-exist diagnosis for an on-disk file:\n%s", out)
	}
}

func TestLintScanIgnoreCanDeadenAScopeExclude(t *testing.T) {
	// Documents a deliberate interaction: scan.ignore covering the same tree
	// as a rule's scope.exclude makes that exclude dead — hygiene then
	// demands the usual justification comment or removal (#55 shape).
	// Discriminating: verified red without the scan: key (exclude is live
	// then, so no hygiene problem fires).
	failed, out := lintTracked(t, map[string]string{
		".formwork/formwork.yaml": "version: 1\nscan:\n  ignore:\n    - glob: 'vendor/**'\n      reason: vendored\n",
		".formwork/rules/r.yaml": "rules:\n  - id: no-banana\n    type: forbidden-pattern\n" +
			"    scope: {include: ['**/*.txt'], exclude: ['vendor/**']}\n    params: {pattern: banana}\n",
		".formwork/fixtures/no-banana/fire-1/f.txt": "banana want: no-banana\n",
		".formwork/fixtures/no-banana/pass-1/f.txt": "clean\n",
		"vendor/x.txt": "x\n", // used to make the exclude live; now ignored (and untracked)
		"notes.txt":    "in scope\n",
	}, ".formwork", "notes.txt")
	if failed == 0 || !strings.Contains(out, "scope.exclude \"vendor/**\" matches no files") {
		t.Fatalf("want dead-exclude hygiene failure once scan.ignore hides the tree, got failed=%d:\n%s", failed, out)
	}
}
