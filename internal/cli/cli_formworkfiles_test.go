package cli_test

// A census detector judging one file at a time needs to know which files a
// changeset run is about: FORMWORK_FILES lists the changed in-scope files under
// --staged and --range, and is unset on the whole-tree run that backstops
// every changeset skip.

import (
	"path/filepath"
	"strings"
	"testing"
)

const listingCensus = `rules:
  - id: census-lists
    type: forbidden-pattern
    scope: {include: ['**/*.go']}
    params:
      census:
        cmd: [sh, -c, 'printf "files<%s>" "$(cat "${FORMWORK_FILES:-/dev/null}")"; [ -n "${FORMWORK_FILES-}" ] || printf "unset"; exit 1']
`

func listingRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".formwork", "formwork.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(dir, ".formwork", "rules", "rules.yaml"), listingCensus)
	mustWrite(t, filepath.Join(dir, "a.go"), "package p\n")
	mustWrite(t, filepath.Join(dir, "b.go"), "package p\n")
	gitInit(t, dir)
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-q", "-m", "seed")
	return dir
}

func TestStagedCensusIsHandedOnlyTheStagedFiles(t *testing.T) {
	dir := listingRepo(t)
	mustWrite(t, filepath.Join(dir, "b.go"), "package p\n\n// edited\n")
	gitRun(t, dir, "add", "b.go")
	_, out, errOut := runCLI(t, "check", "-C", dir, "--staged", "--skip-escapes")
	if !strings.Contains(out, "files<b.go>") {
		t.Fatalf("a staged run lists the staged in-scope file and nothing else:\n%s%s", out, errOut)
	}
}

func TestWholeTreeCensusIsHandedNoListing(t *testing.T) {
	dir := listingRepo(t)
	_, out, errOut := runCLI(t, "check", "-C", dir, "--skip-escapes")
	if !strings.Contains(out, "unset") {
		t.Fatalf("a whole-tree run sets no listing, so the detector judges the root:\n%s%s", out, errOut)
	}
}
