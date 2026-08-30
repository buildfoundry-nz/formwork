package cli_test

// rules-for × scan.gitignore end-to-end tests (#122), split from
// rulesfor_test.go when the 750-line vendor cap fired. Shares runCLI
// (cli_test.go), writeFile (introspect_test.go), and gitignoreRepo/git
// (gitignore_check_test.go).

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitignoreQueryRepo is gitignoreRepo (gitignore_check_test.go — the no-banana
// rule, scan.gitignore declared) made queryable: git initialized, vendor/
// git-ignored, and an ignored file on disk.
func gitignoreQueryRepo(t *testing.T) string {
	t.Helper()
	root := gitignoreRepo(t, true)
	git(t, root, "init", "-q")
	writeFile(t, root, ".gitignore", "vendor/\n")
	writeFile(t, root, "vendor/gen.go", "package gen\n")
	return root
}

func TestRulesForGitIgnoredPathNotScanned(t *testing.T) {
	// The walk's third hiding channel (#100): with scan.gitignore declared, a
	// git-ignored path is pruned before any rule sees it. A governing-rules
	// answer here would assert enforcement check never performs — the exact
	// display-disagrees-with-verdict class #122 names. The answer must carry
	// the deciding ignore rule in the census's own <file>:<line>:<pattern>
	// shape and the operator's declared reason.
	root := gitignoreQueryRepo(t)
	code, out, _ := runCLI(t, "rules-for", "-C", root, "vendor/gen.go")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	for _, needed := range []string{"not scanned", "scan.gitignore", ".gitignore:1:vendor/", "git already refuses these"} {
		if !strings.Contains(strings.ToLower(out), strings.ToLower(needed)) {
			t.Fatalf("gitignored path answer missing %q:\n%s", needed, out)
		}
	}
	if strings.Contains(out, "no-banana") {
		t.Fatalf("must not list rules as governing a git-pruned path:\n%s", out)
	}
}

func TestRulesForGitIgnoredJSONIsStructural(t *testing.T) {
	root := gitignoreQueryRepo(t)
	code, out, _ := runCLI(t, "rules-for", "-C", root, "-format", "json", "vendor/gen.go")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	var got []struct {
		Rules      []any `json:"rules"`
		NotScanned *struct {
			By     string `json:"by"`
			Glob   string `json:"glob"`
			Rule   string `json:"rule"`
			Reason string `json:"reason"`
		} `json:"not_scanned"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if len(got) != 1 || got[0].NotScanned == nil {
		t.Fatalf("not_scanned must be structural: %s", out)
	}
	ns := got[0].NotScanned
	if ns.By != "scan.gitignore" || ns.Rule != ".gitignore:1:vendor/" || ns.Glob != "" ||
		ns.Reason != "git already refuses these" || len(got[0].Rules) != 0 {
		t.Fatalf("wrong not_scanned shape: %+v", ns)
	}
}

func TestRulesForTrackedUnderIgnoredDirStaysGoverned(t *testing.T) {
	// Git's own carve-out (#120 design): a tracked file is never ignored,
	// whatever .gitignore says, so the walk scans it and rules-for must keep
	// its governing-rules answer. The untracked sibling stays NOT SCANNED —
	// via the files set this time, since a dir holding a tracked file cannot
	// collapse to a dir prune.
	root := gitignoreQueryRepo(t)
	writeFile(t, root, "vendor/kept.go", "package gen\n")
	git(t, root, "add", "-f", "vendor/kept.go")
	code, out, _ := runCLI(t, "rules-for", "-C", root, "vendor/kept.go", "vendor/gen.go")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	kept := section(t, out, "vendor/kept.go", "vendor/gen.go")
	rest := section(t, out, "vendor/gen.go", "")
	if !strings.Contains(kept, "no-banana") || strings.Contains(strings.ToLower(kept), "not scanned") {
		t.Fatalf("force-added file must stay governed:\n%s", out)
	}
	if !strings.Contains(strings.ToLower(rest), "not scanned") || strings.Contains(rest, "no-banana") {
		t.Fatalf("untracked ignored sibling must stay hidden:\n%s", out)
	}
}

func TestRulesForGitignoreUndeclaredKeepsGovernedAnswer(t *testing.T) {
	// Key absent = channel off: whatever .gitignore says, the walk scans
	// everything, so the governed answer stands — byte-identical behavior to
	// before the key existed.
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	gitRun(t, root, "init", "-q")
	writeFile(t, root, ".formwork/formwork.yaml", "version: 1\n")
	writeFile(t, root, ".formwork/rules/main.yaml", `rules:
  - id: no-todo
    type: forbidden-pattern
    severity: error
    scope:
      include: ["**/*.go"]
    params:
      pattern: 'TODO'
`)
	writeFile(t, root, ".gitignore", "vendor/\n")
	writeFile(t, root, "vendor/gen.go", "package gen\n")
	code, out, _ := runCLI(t, "rules-for", "-C", root, "vendor/gen.go")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if !strings.Contains(out, "no-todo") || strings.Contains(strings.ToLower(out), "not scanned") {
		t.Fatalf("undeclared key must leave the governed answer untouched:\n%s", out)
	}
}

func TestRulesForGitignoreUnanswerableIsLoud(t *testing.T) {
	// scan.gitignore declared but git cannot answer (no repository here).
	// check degrades softly — pruning nothing only widens its scan — but a
	// governance ANSWER that silently dropped a declared prune channel could
	// assert scanning that the enforcing environment never performs. Loud
	// exit 2, never a confident answer (issue #122 acceptance; vcs package
	// contract: an error is never read as "nothing is ignored").
	root := t.TempDir()
	writeFile(t, root, ".formwork/formwork.yaml", `version: 1
scan:
  gitignore:
    reason: "git refuses these paths; the walk agrees"
`)
	writeFile(t, root, ".formwork/rules/main.yaml", `rules:
  - id: no-todo
    type: forbidden-pattern
    severity: error
    scope:
      include: ["**/*.go"]
    params:
      pattern: 'TODO'
`)
	writeFile(t, root, "vendor/gen.go", "package gen\n")
	code, _, errOut := runCLI(t, "rules-for", "-C", root, "vendor/gen.go")
	if code != 2 {
		t.Fatalf("exit %d, want 2 when the declared channel is unanswerable\nstderr:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "scan.gitignore") {
		t.Fatalf("refusal must name the channel:\n%s", errOut)
	}
}

func TestRulesForGhostGitIgnoredPathNotScanned(t *testing.T) {
	// The ghost frame (fail-open review, this branch): GitIgnored snapshots
	// only paths that EXIST, but guidance is asked about files not yet
	// written — rules-for's central use case. A ghost covered by a
	// .gitignore pattern must answer NOT SCANNED via git's own pattern
	// evaluation (check-ignore), never a governed answer the walk will
	// contradict the moment the file lands. vendor/ holds a force-added file
	// so it does NOT collapse into the dirs snapshot — the miss is real.
	root := gitignoreQueryRepo(t)
	writeFile(t, root, ".gitignore", "vendor/\n*.bin\n")
	writeFile(t, root, "vendor/kept.go", "package gen\n")
	git(t, root, "add", "-f", "vendor/kept.go")
	code, out, _ := runCLI(t, "rules-for", "-C", root, "vendor/future.go", "newdir/x.bin", "src/new.go")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	future := section(t, out, "vendor/future.go", "newdir/x.bin")
	bin := section(t, out, "newdir/x.bin", "src/new.go")
	free := section(t, out, "src/new.go", "")
	if !strings.Contains(future, ".gitignore:1:vendor/") || strings.Contains(future, "no-banana") {
		t.Fatalf("ghost under ignored dir must be NOT SCANNED with the deciding line:\n%s", out)
	}
	if !strings.Contains(bin, ".gitignore:2:*.bin") || strings.Contains(bin, "no-banana") {
		t.Fatalf("ghost under leaf pattern must be NOT SCANNED with the deciding line:\n%s", out)
	}
	// The guard against over-hiding: a ghost no pattern matches keeps its
	// governed answer — that is the whole point of the glob frame.
	if !strings.Contains(free, "no-banana") || strings.Contains(strings.ToLower(free), "not scanned") {
		t.Fatalf("unmatched ghost must stay governed:\n%s", out)
	}
}

func TestRulesForGhostAttributionIsShallowestFirst(t *testing.T) {
	// #125 review finding 2: with a .gitignore dir pattern at a SHALLOW level
	// and a scan.ignore glob at a DEEPER one, nothing yet on disk, the walk —
	// once the file lands — prunes at the shallow gitignored level, and the
	// census attributes the .gitignore line. Guidance must name that same
	// channel and reason, not the deeper glob's: the consumer acts on which
	// declaration to edit to bring the path into scope.
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	git(t, root, "init", "-q")
	writeFile(t, root, ".formwork/formwork.yaml", `version: 1
scan:
  ignore:
    - glob: "a/b/**"
      reason: "deep operator carve-out"
  gitignore:
    reason: "git already refuses these"
`)
	writeFile(t, root, ".formwork/rules/main.yaml", `rules:
  - id: no-todo
    type: forbidden-pattern
    severity: error
    scope:
      include: ["**/*.go"]
    params:
      pattern: 'TODO'
`)
	writeFile(t, root, ".gitignore", "a/\n")
	code, out, _ := runCLI(t, "rules-for", "-C", root, "a/b/x.go")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if !strings.Contains(out, ".gitignore:1:a/") || !strings.Contains(out, "scan.gitignore") {
		t.Fatalf("shallow gitignore level must win attribution:\n%s", out)
	}
	if strings.Contains(out, "deep operator carve-out") || strings.Contains(out, "a/b/**") {
		t.Fatalf("deeper glob must not be attributed over the shallower gitignore prune:\n%s", out)
	}
}

func TestRulesForNegatedGitignorePatternStaysGoverned(t *testing.T) {
	// #125 review finding 1: check-ignore -v emits a record (exit 0) for a
	// path whose DECIDING pattern is a negation — git saying "explicitly not
	// ignored". Treating that as an ignore verdict answers NOT SCANNED for a
	// path the walk scans and enforces on, with a "deciding rule" that
	// pasted back at git proves the opposite. The negated ghost stays
	// governed; its non-negated sibling stays hidden.
	root := gitignoreRepo(t, true)
	git(t, root, "init", "-q")
	writeFile(t, root, ".gitignore", "*.log\n!important.log\n")
	code, out, _ := runCLI(t, "rules-for", "-C", root, "important.log", "debug.log")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	imp := section(t, out, "important.log", "debug.log")
	dbg := section(t, out, "debug.log", "")
	if !strings.Contains(imp, "no-banana") || strings.Contains(strings.ToLower(imp), "not scanned") {
		t.Fatalf("negation-carved ghost must stay governed:\n%s", out)
	}
	if !strings.Contains(dbg, ".gitignore:1:*.log") || strings.Contains(dbg, "no-banana") {
		t.Fatalf("non-negated sibling must stay hidden with the deciding line:\n%s", out)
	}
}

// section slices out[from:to) with loud diagnostics — a missing marker is a
// test failure with the full output, never an index panic (#125 round-2).
func section(t *testing.T, out, from, to string) string {
	t.Helper()
	i := strings.Index(out, from)
	if i < 0 {
		t.Fatalf("missing %q in:\n%s", from, out)
	}
	rest := out[i:]
	if to == "" {
		return rest
	}
	j := strings.Index(rest, to)
	if j < 0 {
		t.Fatalf("missing %q after %q in:\n%s", to, from, out)
	}
	return rest[:j]
}

func TestRulesForGitignoredSymlinkAncestorNamesChannel(t *testing.T) {
	// #125 round-2 finding 1, end to end: git lists a symlink-to-directory
	// as a non-dir entry, and the walk prunes it in its FILE branch at the
	// gitignore check — censused as scan.gitignore. The guidance answer must
	// name that same channel, not diagnose the symlink shape: the two
	// trusted surfaces may never disagree about who hid a path.
	root := gitignoreRepo(t, true)
	git(t, root, "init", "-q")
	writeFile(t, root, ".gitignore", "symdir\n")
	writeFile(t, root, "realdir/x.go", "package x\n")
	if err := os.Symlink("realdir", filepath.Join(root, "symdir")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	code, out, _ := runCLI(t, "rules-for", "-C", root, "symdir/x.go")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if !strings.Contains(out, "scan.gitignore") || !strings.Contains(out, ".gitignore:1:symdir") {
		t.Fatalf("gitignored symlink ancestor must attribute the gitignore channel, as the census does:\n%s", out)
	}
	if strings.Contains(out, "not a regular file") {
		t.Fatalf("must not fall through to the non-regular diagnosis:\n%s", out)
	}
}

func TestRulesForGhostLeafVerdictDoesNotLeakAcrossQueries(t *testing.T) {
	// #125 round-3 finding 1: a ghost-leaf candidate is asked in the FILE
	// frame, and that verdict is valid only at that query's own leaf. With
	// .gitignore "x" + "!x/" (file x ignored, directory x explicitly
	// re-included), the answer for x/y.go must not depend on whether "x"
	// was also queried in the same invocation.
	root := gitignoreRepo(t, true)
	git(t, root, "init", "-q")
	writeFile(t, root, ".gitignore", "x\n!x/\n")

	code, out, _ := runCLI(t, "rules-for", "-C", root, "x/y.go")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if !strings.Contains(out, "no-banana") || strings.Contains(strings.ToLower(out), "not scanned") {
		t.Fatalf("dir-frame negation re-includes x/ — ghost under it is governed:\n%s", out)
	}

	code, out, _ = runCLI(t, "rules-for", "-C", root, "x", "x/y.go")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	xSec := section(t, out, "x:", "x/y.go:")
	ySec := section(t, out, "x/y.go:", "")
	if !strings.Contains(xSec, ".gitignore:1:x") {
		t.Fatalf("the file x itself is git-ignored:\n%s", out)
	}
	if !strings.Contains(ySec, "no-banana") || strings.Contains(strings.ToLower(ySec), "not scanned") {
		t.Fatalf("query x's file-frame verdict must not hide query x/y.go's ancestor:\n%s", out)
	}
}

func TestRulesForSnapshotUnansweredDefersToGitFreeVerdicts(t *testing.T) {
	// #125 round-3 finding 2: the snapshot-resolve failure gets the same
	// precedence carve-out as the ghost-batch failure (spec §10): a verdict
	// decidable without git — here a scan.ignore glob — still answers, and
	// only a query whose verdict depends on the unanswerable channel
	// refuses. This repo is deliberately NOT a git repository.
	root := t.TempDir()
	writeFile(t, root, ".formwork/formwork.yaml", `version: 1
scan:
  ignore:
    - glob: "third_party/**"
      reason: "vendored, not ours"
  gitignore:
    reason: "git already refuses these"
`)
	writeFile(t, root, ".formwork/rules/main.yaml", `rules:
  - id: no-todo
    type: forbidden-pattern
    severity: error
    scope:
      include: ["**/*.go"]
    params:
      pattern: 'TODO'
`)
	writeFile(t, root, "src/app.go", "package app\n")

	code, out, _ := runCLI(t, "rules-for", "-C", root, "third_party/newfile.go")
	if code != 0 || !strings.Contains(out, "third_party/**") {
		t.Fatalf("glob verdict needs no git — exit %d:\n%s", code, out)
	}

	code, _, errOut := runCLI(t, "rules-for", "-C", root, "src/app.go")
	if code != 2 || !strings.Contains(errOut, "scan.gitignore") {
		t.Fatalf("a governed answer depends on the unresolved snapshot — exit %d, stderr:\n%s", code, errOut)
	}
}

func TestRulesForGhostNegationCarvedDescendantStaysGoverned(t *testing.T) {
	// Opus parity sweep finding: with the common `dir/*` + `!dir/keep/`
	// idiom and nothing on disk, the dir-frame candidate "gen/" matches the
	// positive pattern AS A STRING, but git's verdict for the full path —
	// which resolves the negation — is not-ignored, and check scans and
	// enforces the written file. The ghost gitignore conclusion must be
	// gated on git's leaf verdict, never on an unvetted dir-frame match.
	root := gitignoreRepo(t, true)
	git(t, root, "init", "-q")
	writeFile(t, root, ".gitignore", "gen/*\n!gen/keep/\n")
	code, out, _ := runCLI(t, "rules-for", "-C", root, "gen/keep/a.go", "gen/x.go")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	keep := section(t, out, "gen/keep/a.go:", "gen/x.go:")
	x := section(t, out, "gen/x.go:", "")
	if !strings.Contains(keep, "no-banana") || strings.Contains(strings.ToLower(keep), "not scanned") {
		t.Fatalf("negation-carved ghost descendant must stay governed:\n%s", out)
	}
	if !strings.Contains(x, ".gitignore:1:gen/*") || strings.Contains(x, "no-banana") {
		t.Fatalf("the genuinely ignored sibling stays hidden with the deciding line:\n%s", out)
	}
}

func TestRulesForGhostUnderCollapsedDirFollowsPostLandingFrame(t *testing.T) {
	// Opus parity sweep observation: foo/ is wholly ignored TODAY (so the
	// snapshot collapses it), but the queried ghost is negation-carved —
	// the moment it lands, the collapse dissolves and check scans it. The
	// ghost answer speaks about that moment (spec §10: never an answer the
	// walk will contradict the moment it lands), so it is governed.
	root := gitignoreRepo(t, true)
	git(t, root, "init", "-q")
	writeFile(t, root, ".gitignore", "foo/*\n!foo/bar/\n")
	writeFile(t, root, "foo/junk.txt", "ignored\n")
	code, out, _ := runCLI(t, "rules-for", "-C", root, "foo/bar/baz.go")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if !strings.Contains(out, "no-banana") || strings.Contains(strings.ToLower(out), "not scanned") {
		t.Fatalf("ghost carved out of a currently-collapsed dir is governed the moment it lands:\n%s", out)
	}
}

// gitlink plants a submodule entry (mode 160000) in the index without
// needing a real nested repository: check-ignore refuses pathspecs under a
// registered submodule with a FATAL (exit 128), which is the failure shape
// under test.
func gitlink(t *testing.T, root, at string) {
	t.Helper()
	git(t, root, "config", "user.email", "t@example.com")
	git(t, root, "config", "user.name", "Test")
	git(t, root, "config", "commit.gpgsign", "false")
	// Same detached-maintenance disarm gitInit carries (see cli_test.go):
	// the commit below forks it otherwise.
	git(t, root, "config", "maintenance.auto", "false")
	writeFile(t, root, "seed.txt", "seed\n")
	git(t, root, "add", "seed.txt")
	git(t, root, "commit", "-q", "-m", "seed")
	sha := make([]byte, 0)
	_ = sha
	out, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	git(t, root, "update-index", "--add", "--cacheinfo", "160000,"+strings.TrimSpace(string(out))+","+at)
}

func TestRulesForSubmodulePathsAnswerAndDoNotPoisonSiblings(t *testing.T) {
	// Opus adversarial sweep finding: check-ignore exits 128 for any path
	// under a registered submodule, and the shared batch turned that into
	// exit 2 for EVERY query in the invocation. The walk is
	// submodule-oblivious — check scans and enforces files written there —
	// so guidance answers governed, and an unrelated sibling's answer never
	// depends on what else is in argv.
	root := gitignoreRepo(t, true)
	git(t, root, "init", "-q")
	writeFile(t, root, ".gitignore", "dist/\n")
	gitlink(t, root, "sub")

	code, out, errOut := runCLI(t, "rules-for", "-C", root, "sub/new.go")
	if code != 0 {
		t.Fatalf("solo submodule ghost: exit %d\nstderr: %s", code, errOut)
	}
	if !strings.Contains(out, "no-banana") || strings.Contains(strings.ToLower(out), "not scanned") {
		t.Fatalf("the walk is submodule-oblivious — governed:\n%s", out)
	}

	code, out, errOut = runCLI(t, "rules-for", "-C", root, "src/new.go", "sub/new.go", "dist/x.go")
	if code != 0 {
		t.Fatalf("mixed argv: exit %d\nstderr: %s", code, errOut)
	}
	src := section(t, out, "src/new.go:", "sub/new.go:")
	sub := section(t, out, "sub/new.go:", "dist/x.go:")
	dist := section(t, out, "dist/x.go:", "")
	if !strings.Contains(src, "no-banana") || !strings.Contains(sub, "no-banana") {
		t.Fatalf("no argument may poison a sibling's answer:\n%s", out)
	}
	if !strings.Contains(dist, ".gitignore:1:dist/") {
		t.Fatalf("the genuinely ignored ghost still answers:\n%s", out)
	}
}

func TestRulesForGlobHiddenQueryCollectsNoGhostCandidates(t *testing.T) {
	// The pass-2 promise — only a query that actually NEEDS the ghost
	// answer refuses — requires pass 1 to skip candidates for queries the
	// git-free channels already hide: here the submodule path is covered by
	// a scan.ignore glob, so no check-ignore candidate for it may exist to
	// poison the batch.
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	git(t, root, "init", "-q")
	writeFile(t, root, ".formwork/formwork.yaml", `version: 1
scan:
  ignore:
    - glob: "sub/**"
      reason: "vendored submodule, not ours"
  gitignore:
    reason: "git already refuses these"
`)
	writeFile(t, root, ".formwork/rules/main.yaml", `rules:
  - id: no-todo
    type: forbidden-pattern
    severity: error
    scope:
      include: ["**/*.go"]
    params:
      pattern: 'TODO'
`)
	gitlink(t, root, "sub")
	code, out, errOut := runCLI(t, "rules-for", "-C", root, "sub/ghost.go", "src/new.go")
	if code != 0 {
		t.Fatalf("exit %d\nstderr: %s", code, errOut)
	}
	sub := section(t, out, "sub/ghost.go:", "src/new.go:")
	src := section(t, out, "src/new.go:", "")
	if !strings.Contains(sub, "sub/**") {
		t.Fatalf("glob answer stands without git:\n%s", out)
	}
	if !strings.Contains(src, "no-todo") {
		t.Fatalf("sibling governed answer must survive:\n%s", out)
	}
}
