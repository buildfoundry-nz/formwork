// action_pins_test.go — every third-party action this repository runs is
// pinned to a full commit SHA.
//
// A tag is a mutable pointer. `actions/checkout@v4` is whatever the v4 tag
// points at the next time a job starts, and the account that publishes the
// action decides that, not this repository. release.yml runs with
// `contents: write` and publishes the binaries people install; a moved tag in
// that job is a supply-chain compromise with a GitHub Release at the end of it.
// Nothing about the workflow file changes when it happens, which is what makes
// it worth a gate rather than a convention.
//
// WHAT A SHA PIN COSTS, and how that cost is paid. It rots instead of moving:
// a pinned action silently ages out of security support because nothing
// proposes bumps. .github/dependabot.yml is the other half — one reviewable
// pull request per action, through the same code-owner-reviewed gate as any
// other change — and this file requires it to exist, because a SHA-pinning
// policy with nothing bumping the SHAs is a policy that expires.
//
// LOCAL ACTIONS ARE NOT PINNED AND MUST NOT BE. `./.github/actions/setup-go`
// is this repository's own file at the commit being tested; there is no third
// party and no SHA to name. The predicate keys on the leading `./`, which is
// the syntax GitHub itself uses to mean "from this checkout".
package repoproof_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A pinned use looks like `owner/repo@<40 hex>`, optionally followed by a
// comment naming the human-readable version. The version comment is not
// optional in practice — a 40-hex string is unreadable and Dependabot keeps
// the comment in step — but requiring it here would fail a correct pin for a
// documentation reason, so it is checked separately below.
var (
	usesLine    = regexp.MustCompile(`^\s*-?\s*uses:\s*(\S+)`)
	pinnedToSHA = regexp.MustCompile(`@[0-9a-f]{40}$`)
)

// workflowLikeFiles is every file that can carry a `uses:` — the workflows and
// the composite actions. Both, because the composite action is where the
// setup-go version is actually chosen, and a gate that read only
// .github/workflows/ would have judged neither of the two files that matter.
func workflowLikeFiles(t *testing.T) []string {
	t.Helper()
	root := repoRoot(t)
	var out []string
	for _, dir := range []string{".github/workflows", ".github/actions"} {
		err := filepath.WalkDir(filepath.Join(root, filepath.FromSlash(dir)), func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if ext := filepath.Ext(p); ext == ".yml" || ext == ".yaml" {
				out = append(out, p)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("cannot walk %s, so this arm read no workflows and fails closed: %v", dir, err)
		}
	}
	return out
}

func TestEveryThirdPartyActionIsPinnedToASHA(t *testing.T) {
	third := 0
	for _, path := range workflowLikeFiles(t) {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		rel, _ := filepath.Rel(repoRoot(t), path)
		for i, line := range strings.Split(string(body), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue
			}
			m := usesLine.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			ref := m[1]
			if strings.HasPrefix(ref, "./") {
				continue // this repository's own action, at the commit under test
			}
			third++
			if !pinnedToSHA.MatchString(ref) {
				t.Errorf("%s:%d uses %s — a MUTABLE ref. Whoever publishes that action can move it\nunder a job that holds `contents: write` and publishes this project's binaries.\nPin the 40-hex commit and keep the version in a trailing `# vN` comment.",
					rel, i+1, ref)
			}
		}
	}
	if third == 0 {
		t.Fatal("no third-party action was examined — this arm read an empty set and would\nreport every workflow pinned over any file at all")
	}
}

// TestSHAPinsHaveSomethingBumpingThem is the half that keeps the pins from
// becoming stale-by-policy. It asks only that the ecosystem is configured, not
// how often: the schedule is a judgement, the presence of one is not.
func TestSHAPinsHaveSomethingBumpingThem(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(repoRoot(t), ".github/dependabot.yml"))
	if err != nil {
		t.Fatalf("no .github/dependabot.yml: %v\nEvery third-party action here is pinned to an immutable SHA, so nothing moves\nit when the action ships a security fix. Without an updater that policy is a\nfreeze, and a frozen dependency is the failure the pinning was protecting\nagainst, arriving more slowly.", err)
	}
	if !strings.Contains(string(body), "package-ecosystem: github-actions") {
		t.Error(".github/dependabot.yml configures no `github-actions` ecosystem, so nothing\nproposes a bump to the SHA pins this package requires.")
	}
}
