// allowlist_spelling_test.go — exemption-hygiene's does-not-exist diagnosis
// against a file the scan really did read, spelled differently (#308's class,
// second instance).
//
// The engine compares an allowlist entry to a finding's path with `==`
// (internal/engine/engine.go:479), so an entry git and the operator's editor
// spell NFC cannot suppress a finding on a directory entry readdir returned
// NFD. The exemption IS inert and exemption-hygiene is right to fail it. What
// it is not right about is WHY: it reports `does not exist` for a file that is
// on disk, in scope, and scanned — sending the operator to create a file they
// are looking at.
//
// This is the same defect the scan.ignore arm beside it already fixed for the
// prune channel (TestLintAllowlistEntryUnderScanIgnoreSaysSoNotDoesNotExist,
// "misleading does-not-exist diagnosis for an on-disk file"). The channel left
// over was normalization, and what makes it closable now is #308:
// scan.(*FileSet).Produced lets the EXISTENCE question be asked of the scan on
// the filesystem's own device+inode oracle, instead of guessed from a string
// map. Narrowly that, and no wider — ignoredByFold's separate NFC/NFD residual
// (lint.go, "a fold that changes encoded length ... can still miss") is about
// WHICH ignore glob hid a path, a question Produced answers false for by
// construction because a pruned path is not in fset.Files at all. A path that
// is both divergently spelled AND hidden by a glob still lands on the plain
// diagnosis; that corner is untouched here.
package meta_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The two byte sequences for one visible filename, written as \u escapes rather
// than as literal characters. A Go source file is saved NFC by every editor
// involved, so a literal on both sides would agree by construction and the test
// would pass over the defect — which is how #97's tests missed this shape, and
// how the throwaway probe that first reproduced THIS one only worked because
// the two literals happened to disagree by accident.
//
// A third copy of the pair (internal/scan/restrict_test.go and
// internal/cli/cli_nfd_test.go hold the others) for the reason cli_nfd_test.go
// gives for being the second: those live in other test packages and are not
// reachable from here, and one shared spelling is not worth exporting test
// fixtures from a production package.
const (
	metaNFCName = "na\u00efve.txt"  // precomposed U+00EF: what an editor writes
	metaNFDName = "nai\u0308ve.txt" // 'i' + combining U+0308: what readdir returns
)

// normalizationInsensitiveFS reports whether this filesystem resolves the NFC
// spelling of a file created with NFD bytes — i.e. whether #98's divergence can
// arise here at all. macOS/APFS: yes. Linux/ext4: no, and there it cannot arise,
// because every reader sees the bytes that were written.
//
// Probed rather than keyed on GOOS: the property belongs to the filesystem.
func normalizationInsensitiveFS(t *testing.T, dir string) bool {
	t.Helper()
	probe := filepath.Join(dir, "probe-"+metaNFDName)
	if err := os.WriteFile(probe, []byte("probe\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := os.Stat(filepath.Join(dir, "probe-"+metaNFCName))
	if rmErr := os.Remove(probe); rmErr != nil {
		t.Fatal(rmErr)
	}
	return err == nil
}

// TestLintAllowlistEntryUnderADivergentSpellingSaysSoNotDoesNotExist is the
// normalization sibling of the scan.ignore test above it in lint_test.go.
//
// hit.txt is here for the same reason it is there: it keeps the allowlist
// otherwise live, so nothing in the verdict can be attributed to an allowlist
// that has no working entry at all.
func TestLintAllowlistEntryUnderADivergentSpellingSaysSoNotDoesNotExist(t *testing.T) {
	probeDir := t.TempDir()
	if !normalizationInsensitiveFS(t, probeDir) {
		t.Skip("filesystem is normalization-sensitive: the entry and the directory entry carry the same bytes, so this divergence cannot arise here")
	}

	failed, out := lint(t, map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml":  exemptLintRule,
		// The allowlist carries the NFC spelling — what an operator's editor
		// writes and what git reports on macOS.
		".formwork/allowlists/legacy.txt":           metaNFCName + "\nhit.txt\n",
		".formwork/fixtures/no-banana/fire-1/f.txt": "banana want: no-banana\n",
		".formwork/fixtures/no-banana/pass-1/f.txt": "clean\n",
		// The DIRECTORY ENTRY carries the NFD spelling — what readdir returns,
		// and therefore what the walk puts in fset.Files.
		metaNFDName: "banana\n",
		"hit.txt":   "banana\n",
	})

	// STILL A FAILURE. The engine matches allowlist paths byte-for-byte, so
	// this entry genuinely cannot suppress anything, and a fix that folded the
	// two spellings HERE would report a working exemption over an engine that
	// still ignores it — a hygiene check certifying the opposite of the run.
	// Only the diagnosis moves.
	if failed == 0 {
		t.Fatalf("want hygiene failure: the entry can never fire, because the engine compares allowlist paths byte-for-byte\n%s", out)
	}
	if strings.Contains(out, metaNFCName+" does not exist") {
		t.Errorf("misleading does-not-exist diagnosis for a file that is on disk and scanned:\n%s", out)
	}
	// The three facts an operator needs to act: the file IS here, the entry
	// cannot match it, and the difference is in the bytes rather than in
	// anything they can see.
	if !strings.Contains(out, "spelled differently on disk") {
		t.Errorf("want a diagnosis naming the spelling divergence:\n%s", out)
	}
	if !strings.Contains(out, "unicode normalization") {
		t.Errorf("want the mechanism named — two visually identical paths differ in bytes:\n%s", out)
	}
	if !strings.Contains(out, "exemption is inert") {
		t.Errorf("want the consequence stated, in the wording the sibling channels use:\n%s", out)
	}
}

// TestLintAllowlistEntryTrulyAbsentStillSaysDoesNotExist is the control, and it
// is what stops the fix above from being "delete the does-not-exist arm".
//
// A path that is on disk under NO spelling must keep the plain diagnosis: the
// cure there is to remove the entry or restore the file, and telling that
// operator their bytes are wrong would send them hunting for a file that is not
// there. Without this arm, answering "spelled differently" unconditionally
// satisfies every assertion in the test above.
func TestLintAllowlistEntryTrulyAbsentStillSaysDoesNotExist(t *testing.T) {
	failed, out := lint(t, map[string]string{
		".formwork/formwork.yaml": "version: 1\n",
		".formwork/rules/r.yaml":  exemptLintRule,
		// Non-ASCII, so the fix's own narrowing is exercised rather than
		// short-circuited by an ASCII gate: absent is absent either way.
		".formwork/allowlists/legacy.txt":           metaNFCName + "\nhit.txt\n",
		".formwork/fixtures/no-banana/fire-1/f.txt": "banana want: no-banana\n",
		".formwork/fixtures/no-banana/pass-1/f.txt": "clean\n",
		"hit.txt": "banana\n",
	})
	if failed == 0 {
		t.Fatalf("want hygiene failure for an entry naming nothing on disk\n%s", out)
	}
	if !strings.Contains(out, metaNFCName+" does not exist") {
		t.Errorf("a path absent under every spelling must keep the plain diagnosis:\n%s", out)
	}
	if strings.Contains(out, "spelled differently on disk") {
		t.Errorf("nothing is on disk here, under any spelling:\n%s", out)
	}
}
