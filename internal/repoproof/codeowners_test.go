// codeowners_test.go — .github/CODEOWNERS is half of the merge gate on main,
// and it is the half that can fail silently.
//
// main's ruleset requires a pull request approved by a CODE OWNER. GitHub
// resolves that requirement per path, against this file: a path no rule
// matches has no owner, and "requires review from a code owner" is then
// satisfied by nobody reviewing it. So the gate's strength is a property of
// this file's text, not of the setting that names it.
//
// AN ORGANIZATION IS NOT AN OWNER. `@org` is accepted by the syntax, rendered
// by the tooling, and resolved by GitHub to nothing — the rule requires the
// approval of an empty set. The validating target's CODEOWNERS is written that
// way on all 44 of its lines and has been for as long as it has existed;
// `codeowners/errors` on that repository answers with 38 "Unknown owner"
// errors, which is the whole file. That is the defect this test is here to
// keep out of the public tree, and it is exactly the shape everything else in
// this package guards: a check that cannot fail.
//
// WHAT THIS TEST CANNOT DO, said plainly rather than implied. Whether the team
// named here exists, and whether it holds write access, is a fact about the
// GitHub organization and not about this tree — `gh api
// repos/<owner>/<repo>/codeowners/errors` is the only thing that answers it,
// and it needs the network and a token. This test holds the half that IS in
// the tree: that the file is here, that it covers every path, and that no
// owner on it is spelled in the form that resolves to nobody.
package repoproof_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const codeownersPath = ".github/CODEOWNERS"

// ownersOn returns the owner tokens on a CODEOWNERS line, or nil if the line
// declares no rule. Comments and blank lines declare nothing.
func ownersOn(line string) (pattern string, owners []string) {
	if i := strings.IndexByte(line, '#'); i >= 0 {
		line = line[:i]
	}
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return "", nil
	}
	return fields[0], fields[1:]
}

func readCodeowners(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(repoRoot(t), filepath.FromSlash(codeownersPath)))
	if err != nil {
		t.Fatalf("cannot read %s, so the merge gate's owner half is unheld: %v\n"+
			"main's ruleset requires review from a code owner; with no such file every\npath resolves to no owner and that requirement asks for nothing.",
			codeownersPath, err)
	}
	return string(body)
}

// TestCodeownersCoversEveryPath is the non-vacuity arm. A CODEOWNERS listing
// only some paths leaves the rest unowned, and an unowned path is one a
// code-owner requirement does not reach.
func TestCodeownersCoversEveryPath(t *testing.T) {
	for _, line := range strings.Split(readCodeowners(t), "\n") {
		if pattern, owners := ownersOn(line); pattern == "*" && len(owners) > 0 {
			return
		}
	}
	t.Fatalf("%s declares no `*` rule, so some path in this repository has no code owner\n"+
		"and main's code-owner review requirement does not reach it. Every narrowing of\nthis file is an exemption; write the paths it should NOT cover, not the ones it should.",
		codeownersPath)
}

// TestCodeownersNamesNoBareOrganization is the arm that catches the defect
// that is live in the validating target. It is deliberately spelled as "the
// owner is the organization" rather than as a list of good owners: a team is
// `@org/team` and a user is `@name`, and the only form that resolves to
// nothing is the org's own login standing alone.
func TestCodeownersNamesNoBareOrganization(t *testing.T) {
	body := readCodeowners(t)

	// The organization is read out of the module path rather than written
	// here, so a fork or a rename cannot leave this arm hunting a name that
	// is no longer the owner's — which would make it pass over anything.
	mod, err := os.ReadFile(filepath.Join(repoRoot(t), "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	org := ""
	for _, l := range strings.Split(string(mod), "\n") {
		if path, ok := strings.CutPrefix(strings.TrimSpace(l), "module "); ok {
			parts := strings.Split(strings.TrimSpace(path), "/")
			if len(parts) >= 2 {
				org = parts[1]
			}
			break
		}
	}
	if org == "" {
		t.Fatal("go.mod declares no module path, so the organization is unknown and this\narm would hunt an empty name and pass over any file at all")
	}

	rules := 0
	for i, line := range strings.Split(body, "\n") {
		pattern, owners := ownersOn(line)
		if len(owners) == 0 {
			continue
		}
		rules++
		for _, owner := range owners {
			if strings.EqualFold(owner, "@"+org) {
				t.Errorf("%s:%d gives %q to %s — an ORGANIZATION, which GitHub resolves to no owner.\nThe rule then requires the approval of an empty set and the file reads as if it\ndoes not. Name a team (@%s/<team>) or a user.",
					codeownersPath, i+1, pattern, owner, org)
			}
		}
	}
	if rules == 0 {
		t.Fatalf("%s declares no rules at all — this arm read an empty file and would\nreport a clean bill over any spelling of any owner", codeownersPath)
	}
}
