// denylist_regen_test.go — a commit that moves a `make denylist` INPUT
// without the outputs that input is written into owes a regeneration (#253).
//
// WHY THIS EXISTS. a8503fd9 added tokens to allow.txt without regenerating
// hashes.txt in the same commit — it could not: regeneration reads the pinned
// clone (internal/denylist.Generate stats cloneRoot and refuses without it),
// and CI has no clone. The stale hashes then produced a false-alarm diff at
// the next `make denylist` that had to be traced through history to prove
// benign. allow.txt is a Generate INPUT (LoadAllow filters which clone tokens
// get hashed), so moving it moves the hash set.
//
// THIS IS A REMINDER, NOT A PROOF. It reads the staged diff and fires when an
// input is staged without them. It cannot verify the hashes are
// right — only `make denylist` against the clone can — so it flags the commit
// SHAPE. With nothing staged (CI checkout) there is no commit shape to judge
// and the guard no-ops green. It never touches the clone.
//
// WHY NOT A git-diff RULE. That type matches added/removed CONTENT lines —
// the +++/--- header lines that carry the file's path are explicitly skipped
// in FinalizeErr — so the path never reaches the matcher, and its
// single-pattern model has no spelling for "allow.txt present AND hashes.txt
// absent from the same diff". forbid_added on allow.txt content would also
// fire when both files move together, which must pass.
package repoproof_test

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const (
	allowlistPath  = "tools/corpus-denylist/allow.txt"
	neverAllowPath = "tools/corpus-denylist/never-allow.txt"
	hashesPath     = "tools/corpus-denylist/hashes.txt"
	structuralPath = "tools/corpus-denylist/structural.txt"
	reposPath      = "repos.txt"
)

// regenInputs maps each `make denylist` INPUT to the outputs a regeneration
// writes from it.
//
// allow.txt and never-allow.txt both feed LoadAllow, which filters which of
// the clone's tokens get hashed, so moving either stales hashes.txt. They do
// not reach structural.txt: writeStructural walks the clone's rule files and
// never consults the allowlist.
//
// repos.txt pins the ref `make sync` checks the pinned clone out at, and
// Generate walks whatever is there — so moving the pin restates the subject of
// BOTH outputs at once (#264 r5). It is the third input, and the only one that
// owes structural.txt.
var regenInputs = map[string][]string{
	allowlistPath:  {hashesPath},
	neverAllowPath: {hashesPath},
	reposPath:      {hashesPath, structuralPath},
}

// regenDebt names, for every generator input in the staged diff, the outputs
// that input stales which the same commit does not carry. An empty map means
// the commit owes nothing: an input staged together with the outputs it
// stales passes (the regeneration travelled with the move), and an output
// staged on its own passes (a regeneration by itself is fine).
func regenDebt(staged []string) map[string][]string {
	present := map[string]bool{}
	for _, p := range staged {
		present[p] = true
	}
	debt := map[string][]string{}
	for in, outs := range regenInputs {
		if !present[in] {
			continue
		}
		var missing []string
		for _, out := range outs {
			if !present[out] {
				missing = append(missing, out)
			}
		}
		if len(missing) > 0 {
			debt[in] = missing
		}
	}
	return debt
}

// stagedPaths lists the paths the commit in progress would carry: the
// staged diff's name-only form.
//
// --no-renames because git detects renames by default and prints a detected
// rename as ONE path, the destination (#264 r3). A staged
// `git mv allow.txt allow-v2.txt` would report allow-v2.txt alone, and the
// guard would go green over the commit that stales hashes.txt hardest —
// the input did not change, it left the path Generate reads. With
// --no-renames the same commit reports both halves and the source path is
// back in the list.
//
// WHEN THIS ACTUALLY FIRES (#264 r4). Nothing in this repository runs these
// tests from a git hook: .git/hooks holds only git's samples, no
// core.hooksPath is set, and the hook bodies `formwork hooks` installs run
// `formwork check`, never `go test`. CI checks out a clean tree, so its
// staged diff is empty and this no-ops green. What is left is the one context
// that does reach it — a developer running `go test ./...`, `make test` or
// `make verify` with a commit already staged. That is a narrow window, and it
// is the window a8503fd9 went through, so it is worth holding; it is not a
// merge gate and this file should not be read as claiming to be one. The
// backstop that always runs is `make corpus-denylist`, which exit-2s when a
// named file is missing.
//
// WHY THIS LOOKUP KEEPS THE ENVIRONMENT WHILE no_shell_test.go SCRUBS IT.
// The two lockdowns in this package ask opposite kinds of question, and the
// difference is the whole reason they are configured opposite ways.
// TestNoTrackedShellScripts asks "what does THIS repository track" — one
// fixed subject, one correct answer, and any GIT_DIR or GIT_INDEX_FILE that
// changes the answer has steered it off its subject, so gitScrubbed removes
// them. This asks "what is the commit in progress", and that answer LIVES in
// the environment: under a partial commit (`git commit -p`, `git commit
// <path>`) git builds a temporary index holding exactly what will be
// committed and points GIT_INDEX_FILE at it. Scrubbing here would read the
// full index instead and judge a commit shape nobody is making.
//
// The honest cost: honouring the pointer means a developer who exports
// GIT_INDEX_FILE at a tree with nothing staged gets the same green as CI. For
// a reminder aimed at the person who set that variable, that is a note to
// self being ignored rather than a gate being bypassed — and the
// `make corpus-denylist` backstop is unaffected either way. The same
// reasoning does not transfer to no_shell_test.go, whose whole verdict is the
// answer.
//
// A git failure is fatal, never a quiet empty answer: this guard failing open
// is how a8503fd9 happened.
func stagedPaths(t *testing.T, dir string) []string {
	t.Helper()
	cmd := exec.Command("git", "diff", "--cached", "--name-only", "--no-renames")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("cannot read the staged diff in %s: %v", dir, err)
	}
	var paths []string
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line != "" {
			paths = append(paths, line)
		}
	}
	return paths
}

// regenOwedMessage composes plumbing and decision: "" when the staged diff
// owes nothing, otherwise the failure text.
func regenOwedMessage(t *testing.T, dir string) string {
	t.Helper()
	debt := regenDebt(stagedPaths(t, dir))
	if len(debt) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, in := range slices.Sorted(maps.Keys(debt)) {
		fmt.Fprintf(&sb, "staged diff moves %s without %s.\n",
			in, strings.Join(debt[in], " and "))
	}
	sb.WriteString("Each of those is an input to `make denylist`, so the committed output(s)\n" +
		"named above are now stale and the next regeneration will show a diff that is\n" +
		"this commit's, not the clone's. Regenerate (`make sync && make denylist`) and\n" +
		"stage them too, or say in the commit message why regeneration was impossible\n" +
		"(CI has no clone) — this check is a reminder, not a proof.")
	return sb.String()
}

func TestRegenDebtDecision(t *testing.T) {
	for _, v := range []struct {
		label  string
		staged []string
		want   bool
	}{
		{"neither file staged", []string{"internal/engine/engine.go"}, false},
		{"nothing staged", nil, false},
		{"both files staged", []string{allowlistPath, hashesPath}, false},
		{"hashes alone staged", []string{hashesPath}, false},
		{"allow alone staged", []string{allowlistPath}, true},
		{"allow staged among others", []string{"Makefile", allowlistPath}, true},
		{"never-allow alone staged", []string{neverAllowPath}, true},
		{"never-allow with hashes staged", []string{neverAllowPath, hashesPath}, false},
		// #264 r5 — repos.txt pins the ref `make sync` checks the clone out
		// at, and Generate walks that clone: a pin move restates the bytes
		// BOTH hashes.txt and structural.txt are written from.
		{"repos pin alone staged", []string{reposPath}, true},
		{"repos pin with hashes but not structural", []string{reposPath, hashesPath}, true},
		{"repos pin with structural but not hashes", []string{reposPath, structuralPath}, true},
		{"repos pin with both outputs staged", []string{reposPath, hashesPath, structuralPath}, false},
		{"structural alone staged", []string{structuralPath}, false},
		// allow.txt feeds LoadAllow, which structural.txt is not written
		// from, so it must not start owing one.
		{"allow with hashes and no structural", []string{allowlistPath, hashesPath}, false},
	} {
		if got := len(regenDebt(v.staged)) > 0; got != v.want {
			t.Errorf("%s: regenDebt(%v) owes = %v, want %v", v.label, v.staged, got, v.want)
		}
	}
}

// gitIn runs one git command in dir and fails the test if it does not succeed.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %v: %s", args, dir, err, out)
	}
}

// buildStagedRepo makes a throwaway repo, commits the denylist files, then
// applies edits and stages exactly the paths named. Both maps are keyed by
// repo-relative path, the same spelling the staged diff reports.
func buildStagedRepo(t *testing.T, edits map[string]string, stage []string) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) { t.Helper(); gitIn(t, dir, args...) }
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "--quiet", "-b", "main", ".")
	run("config", "user.email", "proof@example.invalid")
	run("config", "user.name", "denylist regen proof")
	for rel, body := range map[string]string{
		allowlistPath:  "tok_a\n",
		neverAllowPath: "never_a\n",
		hashesPath:     "hash_a\n",
		structuralPath: "id\tstruct_a\n",
		reposPath:      "pinnedrepo git@example.invalid:t.git 1111111\n",
	} {
		write(rel, body)
	}
	run("add", ".")
	run("commit", "--quiet", "-m", "baseline")
	for rel, body := range edits {
		write(rel, body)
	}
	for _, p := range stage {
		run("add", p)
	}
	return dir
}

func TestRegenOwedMessageOverStagedDiff(t *testing.T) {
	t.Run("allow staged alone fires", func(t *testing.T) {
		dir := buildStagedRepo(t,
			map[string]string{allowlistPath: "tok_a\ntok_b\n"},
			[]string{"tools/corpus-denylist/allow.txt"})
		if msg := regenOwedMessage(t, dir); msg == "" {
			t.Error("allow.txt staged without hashes.txt produced no reminder")
		}
	})
	// never-allow.txt is a generator input too (LoadAllow reads both): its move
	// stales hashes.txt exactly the same way.
	t.Run("never-allow staged alone fires", func(t *testing.T) {
		dir := buildStagedRepo(t,
			map[string]string{neverAllowPath: "never_a\nnever_b\n"},
			[]string{"tools/corpus-denylist/never-allow.txt"})
		msg := regenOwedMessage(t, dir)
		if msg == "" {
			t.Error("never-allow.txt staged without hashes.txt produced no reminder")
		} else if !strings.Contains(msg, "never-allow.txt") {
			t.Errorf("the reminder does not name the file that moved:\n%s", msg)
		}
	})
	// #264 r3 — a rename is the loudest version of the commit shape this
	// guard exists to flag, and it was the one shape it could not see. git
	// detects renames by default and prints ONE path for a detected rename,
	// the destination: a staged `git mv allow.txt allow-v2.txt` reports only
	// allow-v2.txt, so the input the guard watches has disappeared from the
	// diff along with the file itself, and the guard goes green over the
	// commit that stales hashes.txt hardest.
	t.Run("allow.txt renamed away fires", func(t *testing.T) {
		dir := buildStagedRepo(t, nil, nil)
		gitIn(t, dir, "mv", allowlistPath, "tools/corpus-denylist/allow-v2.txt")
		msg := regenOwedMessage(t, dir)
		if msg == "" {
			t.Error("`git mv allow.txt allow-v2.txt`, staged, produced no reminder — rename " +
				"detection erased the source path from the staged diff, so the guard never " +
				"saw its own input move")
		} else if !strings.Contains(msg, "/allow.txt") {
			t.Errorf("the reminder does not name the file that moved:\n%s", msg)
		}
	})
	// #264 r5 — repos.txt was the third regeneration input and nothing
	// watched it. Moving the pin is the largest possible restatement of the
	// generator's subject: every token hashed into hashes.txt and every rule
	// id, pattern, origin and long integer written into structural.txt comes
	// out of the clone at that ref. A staged pin move on its own therefore
	// stales BOTH outputs, and every assertion in this file stayed green
	// over it.
	t.Run("repos pin move staged alone fires and names both outputs", func(t *testing.T) {
		dir := buildStagedRepo(t,
			map[string]string{reposPath: "pinnedrepo git@example.invalid:t.git 2222222\n"},
			[]string{reposPath})
		msg := regenOwedMessage(t, dir)
		if msg == "" {
			t.Fatal("a staged repos.txt pin move produced no reminder — the ref the clone " +
				"is checked out at is what Generate walks, so the move stales hashes.txt " +
				"and structural.txt and nothing said so")
		}
		for _, want := range []string{reposPath, hashesPath, structuralPath} {
			if !strings.Contains(msg, want) {
				t.Errorf("the reminder does not name %s:\n%s", want, msg)
			}
		}
	})
	// Carrying one of the two outputs is not carrying the regeneration:
	// writeStructural reads the same clone hashes.txt was written from.
	t.Run("repos pin move with hashes but not structural still fires", func(t *testing.T) {
		dir := buildStagedRepo(t,
			map[string]string{
				reposPath:  "pinnedrepo git@example.invalid:t.git 3333333\n",
				hashesPath: "hash_a\nhash_b\n",
			},
			[]string{reposPath, hashesPath})
		msg := regenOwedMessage(t, dir)
		if msg == "" || !strings.Contains(msg, structuralPath) {
			t.Errorf("a staged pin move carrying hashes.txt but not structural.txt still "+
				"owes structural.txt, and the reminder must say so:\n%s", msg)
		}
	})
	t.Run("both staged passes", func(t *testing.T) {
		dir := buildStagedRepo(t,
			map[string]string{allowlistPath: "tok_a\ntok_b\n", hashesPath: "hash_a\nhash_b\n"},
			[]string{"tools/corpus-denylist/allow.txt", "tools/corpus-denylist/hashes.txt"})
		if msg := regenOwedMessage(t, dir); msg != "" {
			t.Errorf("allow.txt and hashes.txt staged together still fired:\n%s", msg)
		}
	})
	// Pins the --cached half of the plumbing: an edited-but-unstaged allow.txt
	// is not part of any commit shape and must not fire.
	t.Run("allow edited but unstaged does not fire", func(t *testing.T) {
		dir := buildStagedRepo(t,
			map[string]string{allowlistPath: "tok_a\ntok_b\n"},
			nil)
		if msg := regenOwedMessage(t, dir); msg != "" {
			t.Errorf("unstaged allow.txt edit fired the guard — the staged diff is not what was read:\n%s", msg)
		}
	})
}

// The live guard over THIS repository. Nothing staged — the CI shape — is a
// green no-op; there is no commit in progress to judge.
func TestAllowlistMoveOwesRegeneration(t *testing.T) {
	needBinary(t, "git")
	if msg := regenOwedMessage(t, repoRoot(t)); msg != "" {
		t.Error(msg)
	}
}
