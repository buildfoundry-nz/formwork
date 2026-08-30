package meta_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/meta"
)

// lintRoot is lint() against an already-written repo, so a test can inspect the
// tree afterwards for side effects a rule left behind.
func lintRoot(t *testing.T, root string) (int, string) {
	t.Helper()
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	devOptOutActive, _ := strconv.ParseBool(os.Getenv("FORMWORK_ALLOW_DEV"))
	failed, err := meta.Lint(cfg, root, &sb, devOptOutActive, false)
	if err != nil {
		t.Fatal(err)
	}
	return failed, sb.String()
}

// lint runs the engine only to answer its OWN questions — allowlist staleness
// and the suppressed-finding listing — both of which concern only rules that
// opted in via `except: {marker:}` / `except: {allowlist:}`, plus the prefilter
// rules whose findings prefilterLoadBearing reuses as its base pass.
//
// It must therefore NOT dispatch the heavy `command`/`git-diff` escapes. Those
// belong to `formwork check`, which is the enforcement run. Executing them here
// is pure waste: nothing lint reports reads their findings.
//
// The regression this pins is not hypothetical. The predicate was a whole-config
// ANY over (allowlist || marker), but the run it gated was
// `engine.Run(cfg.Rules, ...)` — every rule. So ONE opted-in rule anywhere made
// lint execute EVERY command rule in the config. In the validating port that
// meant two whole-tree resolved-Dart-AST scans (~1,570 files, a Flutter
// toolchain and a workspace `pub get`) running a second time on every PR,
// docs-only included, for findings nothing read (buildfoundry-nz/formwork#64).
//
// Observability: the command writes a sentinel into the repo root, which is
// `cmd.Dir` for a command rule (internal/rules/command: cmd.Dir = ctx.Root).
// Asserting on lint's OUTPUT could not catch this — the enumeration lists
// command rules as escape hatches whether or not they were executed, which is
// exactly why the waste went unnoticed. The side effect is the only witness.
func TestLintDoesNotExecuteCommandRules(t *testing.T) {
	root := writeRepo(t, map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		// The opted-in rule: its presence alone puts a rule in the engine subset.
		".formwork/rules/r.yaml": "rules:\n" +
			"  - id: no-banana\n    type: forbidden-pattern\n" +
			"    scope: {include: ['**/*.txt']}\n    params: {pattern: banana}\n" +
			"    except: {marker: true, allowlist: allowlists/legacy.txt}\n" +
			// The heavy escape that must stay unexecuted.
			"  - id: heavy-escape\n    type: command\n" +
			"    scope: {include: ['**/*.txt']}\n" +
			"    params: {cmd: [sh, -c, 'printf x > lint-executed.sentinel']}\n",
		".formwork/allowlists/legacy.txt":           "# legacy\nhit.txt\n",
		".formwork/fixtures/no-banana/fire-1/f.txt": "banana want: no-banana\n",
		".formwork/fixtures/no-banana/pass-1/f.txt": "clean\n",
		"hit.txt": "banana\n",
	})

	lintRoot(t, root)

	if _, err := os.Stat(filepath.Join(root, "lint-executed.sentinel")); err == nil {
		t.Fatalf("formwork lint executed a command rule: the heavy escapes belong to `check`, not `lint` — nothing lint reports reads their findings")
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat sentinel: %v", err)
	}
}

// Narrowing the engine run must not break prefilterLoadBearing, which reuses the
// main run's findings as the as-written base for every prefilter-carrying rule.
// A rule with a prefilter but with neither `marker:` nor `allowlist:` is outside
// the opted-in set, so a naive narrowing would hand that check an EMPTY base and
// it would read "the prefilter changed nothing" from findings it never made — a
// false green in the one check that exists to prove prefilters are pure
// optimizations.
//
// Here the prefilter is load-bearing in the wrong direction on purpose: the rule
// fires on content the prefilter literal does not select, so evaluating it with
// and without the prefilter gives DIFFERENT findings, and the check must say so.
// The existing coverage does not reach this case. TestLintFlagsLoadBearingPrefilter
// has no opted-in rule, so there is no main engine run to reuse;
// TestLintPassesRedundantPrefilterWithMainEngineRun does have one, but the
// prefilter rule IS the opted-in rule, so it would sit inside any narrowed
// subset regardless. The uncovered combination is the dangerous one: an opted-in
// rule turns the engine run on, and a DIFFERENT, non-opted-in rule carries the
// prefilter whose findings the differential then reuses as its base.
//
// Narrow the run to (allowlist || marker) alone and this check reads its base
// from a finding set that never contained the prefilter rule — an empty base is
// indistinguishable from "the prefilter changed nothing", so a load-bearing
// prefilter passes silently. That is a false green in the check whose entire
// purpose is proving prefilters are pure optimizations.
func TestLintFlagsLoadBearingPrefilterOnNonOptedInRuleAlongsideAnOptedInOne(t *testing.T) {
	failed, out := lint(t, withFixtures(map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml": prefilterRule +
			// Opted in: its presence alone turns the engine run on, so the
			// differential takes its reuse path rather than its own base pass.
			"  - id: no-banana\n    type: forbidden-pattern\n" +
			"    scope: {include: ['**/*.txt']}\n    params: {pattern: banana}\n" +
			"    except: {marker: true}\n",
		".formwork/fixtures/no-banana/fire-1/f.txt": "banana want: no-banana\n",
		".formwork/fixtures/no-banana/pass-1/f.txt": "clean\n",
		// in scope for the prefiltered rule, matches its pattern, LACKS its
		// prefilter literal — i.e. the prefilter is load-bearing.
		"consts.go": "package p\nvar Category = \"floor\"\n",
		"hit.txt":   "clean\n",
	}))

	if failed == 0 {
		t.Fatalf("a load-bearing prefilter on a non-opted-in rule went unflagged — the differential was handed a base that never contained that rule\n%s", out)
	}
	if !strings.Contains(out, `no-cat-default: prefilter "material_cat" is load-bearing`) {
		t.Fatalf("the offending rule is not named in the verdict:\n%s", out)
	}
}
