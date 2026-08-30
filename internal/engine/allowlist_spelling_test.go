// allowlist_spelling_test.go — an allowlist entry suppresses the finding on the
// file it NAMES, not on the byte sequence it happens to be typed in.
//
// #308's class at the engine. suppressAllowlist compared `e.Path == fd.Path`,
// two strings that on macOS/APFS routinely spell one filename two ways: an
// editor writes the entry NFC (U+00EF), readdir hands the walk the NFD bytes
// ('i' + U+0308) the file was created with, and the exemption an operator wrote
// in good faith suppresses nothing at all — silently, because a finding that is
// merely NOT suppressed reads exactly like a finding nobody exempted.
//
// The oracle is the filesystem, asked through scan.(*FileSet).Produced, the
// same seam Restrict/foldSpellings answers #98 with and the same seam
// internal/meta's exemption-hygiene lint asks. It is device+inode identity, not
// a unicode table, and it is gated on non-ASCII on BOTH sides so no ASCII
// verdict can move.
//
// Its own file rather than exempt_test.go's: these tests need a REAL tree on a
// REAL filesystem (t.TempDir + scan.Walk), where every test there runs on
// memFileSet, and the divergence cannot exist in a memFileSet by construction —
// both spellings there are whatever the Go source literal said.
package engine_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/engine"
	"github.com/buildfoundry-nz/formwork/internal/finding"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// The two byte sequences for one visible filename, written as \u escapes for
// the reason internal/scan/restrict_test.go gives: a Go source file is saved
// NFC by every editor involved, so a test built from literal characters agrees
// with itself by construction and can never see this bug.
const (
	spellNFC = "na\u00efve.go"  // precomposed U+00EF: what an editor types
	spellNFD = "nai\u0308ve.go" // 'i' + combining U+0308: what readdir returns
	spellASC = "plain.go"       // no non-ASCII byte, so nothing to diverge
	spellOff = "autre\u00e9.go" // non-ASCII, and on disk under no spelling
	// spellDcy is on disk, non-ASCII, named by no entry, and sorts BEFORE
	// spellNFD ('a' < 'n'). It is what stops sort order standing in for
	// identity: a fold that took the first walked file it looked at rather
	// than the one the entry names would land here, and every assertion in
	// this file that did not carry a decoy would still pass. Mutating the
	// per-file identity ask to `true` proved exactly that.
	spellDcy = "aper\u00e7u.go"
)

// normalizationInsensitiveFS reports whether this filesystem resolves the NFC
// spelling of a file created with NFD bytes — i.e. whether the divergence can
// arise here at all, and whether the filesystem can therefore serve as the
// oracle for it. macOS/APFS: yes. Linux/ext4: no, and there the divergence does
// not arise either, because every producer reports the bytes readdir gave it.
//
// Probed rather than keyed on GOOS, mirroring internal/scan/restrict_test.go:
// the property belongs to the filesystem, not to the operating system.
func normalizationInsensitiveFS(t *testing.T, dir string) bool {
	t.Helper()
	probe := filepath.Join(dir, "probe-"+spellNFD)
	writeSpellingFile(t, probe, "probe\n")
	_, err := os.Stat(filepath.Join(dir, "probe-"+spellNFC))
	if rmErr := os.Remove(probe); rmErr != nil {
		t.Fatal(rmErr)
	}
	return err == nil
}

func writeSpellingFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// walkedTree writes files and returns the real FileSet a walk produces over
// them. Real, not memFileSet: Produced's oracle is device+inode, which only a
// file that exists has.
func walkedTree(t *testing.T, files map[string]string) *scan.FileSet {
	t.Helper()
	root := t.TempDir()
	if !normalizationInsensitiveFS(t, root) {
		t.Skip("filesystem is normalization-sensitive: every producer reports the bytes readdir gave it, so this divergence cannot arise here")
	}
	for name, content := range files {
		writeSpellingFile(t, filepath.Join(root, name), content)
	}
	fset, err := scan.Walk(root)
	if err != nil {
		t.Fatal(err)
	}
	return fset
}

// byPath indexes findings for assertions that do not want to depend on sort
// order between two spellings of one name.
func byPath(t *testing.T, got []finding.Finding) map[string]finding.Finding {
	t.Helper()
	out := make(map[string]finding.Finding, len(got))
	for _, fd := range got {
		out[fd.Path] = fd
	}
	return out
}

// TestRunAllowlistSuppressesDivergentSpelling is #308 at the engine: the entry
// is spelled NFC, the walk carries NFD, and the two name one inode. Before the
// fold, the finding came back live — the operator's exemption was inert and
// said nothing about being inert.
func TestRunAllowlistSuppressesDivergentSpelling(t *testing.T) {
	fset := walkedTree(t, map[string]string{spellNFD: "x\n", spellASC: "x\n", spellDcy: "x\n"})
	r := mustRule(t, "no-hit", finding.SeverityError, []string{"**"}, matchLineOne())
	r.Allowlist = &config.Allowlist{
		File:    "allowlists/legacy.txt",
		Entries: []config.AllowlistEntry{{Path: spellNFC, Line: 7}},
	}
	got, err := engine.Run([]*config.Rule{r}, fset, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("findings = %+v", got)
	}
	idx := byPath(t, got)
	fd, ok := idx[spellNFD]
	if !ok {
		t.Fatalf("walk did not carry the NFD spelling: %+v", got)
	}
	if !fd.Suppressed {
		t.Fatalf("allowlist entry spelled NFC did not suppress the NFD-walked finding it names: %+v", fd)
	}
	if fd.SuppressedBy != "allowlist:allowlists/legacy.txt:7" {
		t.Fatalf("attribution lost the entry that answered: %+v", fd)
	}
	for _, unnamed := range []string{spellASC, spellDcy} {
		if other := idx[unnamed]; other.Suppressed {
			t.Fatalf("fold widened onto %s, which no entry names: %+v", unnamed, other)
		}
	}
}

// TestRunAllowlistFoldResolvesPerEntry pins the ATTRIBUTION half. Two entries,
// two files, one of them divergently spelled: each finding must carry the line
// of the entry that answered for IT. A fold that suppressed correctly but
// attributed the wrong line would send an operator to the wrong line of the
// allowlist to remove an exemption.
func TestRunAllowlistFoldResolvesPerEntry(t *testing.T) {
	fset := walkedTree(t, map[string]string{spellNFD: "x\n", spellASC: "x\n", spellDcy: "x\n"})
	r := mustRule(t, "no-hit", finding.SeverityError, []string{"**"}, matchLineOne())
	r.Allowlist = &config.Allowlist{
		File: "allowlists/legacy.txt",
		Entries: []config.AllowlistEntry{
			{Path: spellASC, Line: 2},
			{Path: spellNFC, Line: 9},
		},
	}
	got, err := engine.Run([]*config.Rule{r}, fset, 1)
	if err != nil {
		t.Fatal(err)
	}
	idx := byPath(t, got)
	if fd := idx[spellASC]; !fd.Suppressed || fd.SuppressedBy != "allowlist:allowlists/legacy.txt:2" {
		t.Fatalf("exact-spelling entry: %+v", fd)
	}
	if fd := idx[spellNFD]; !fd.Suppressed || fd.SuppressedBy != "allowlist:allowlists/legacy.txt:9" {
		t.Fatalf("divergent-spelling entry: %+v", fd)
	}
	if fd := idx[spellDcy]; fd.Suppressed {
		t.Fatalf("decoy suppressed: neither entry names it: %+v", fd)
	}
}

// TestRunAllowlistFoldDoesNotWiden is the other direction, and it is the one
// that keeps the fold from becoming a fuzzy match. An entry that names no file
// on disk under any spelling suppresses nothing; an entry that names a
// DIFFERENT file suppresses nothing. Identity, not similarity.
func TestRunAllowlistFoldDoesNotWiden(t *testing.T) {
	fset := walkedTree(t, map[string]string{spellNFD: "x\n", spellASC: "x\n", spellDcy: "x\n"})
	r := mustRule(t, "no-hit", finding.SeverityError, []string{"**"}, matchLineOne())
	r.Allowlist = &config.Allowlist{
		File:    "allowlists/legacy.txt",
		Entries: []config.AllowlistEntry{{Path: spellOff, Line: 4}},
	}
	got, err := engine.Run([]*config.Rule{r}, fset, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("findings = %+v", got)
	}
	for _, fd := range got {
		if fd.Suppressed {
			t.Fatalf("entry naming no file on disk suppressed %s: %+v", fd.Path, fd)
		}
	}
}

// TestRunAllowlistSuppressesDivergentSpellingForFinalizerFinding is the second
// caller. suppressAllowlist is reached from two places — evalFile's per-file
// path and the finalizer loop, which has no *scan.File to hand — and a fold
// wired into only the first would leave every whole-tree rule's file-level
// finding on the old byte equality.
func TestRunAllowlistSuppressesDivergentSpellingForFinalizerFinding(t *testing.T) {
	fset := walkedTree(t, map[string]string{spellNFD: "x\n", spellDcy: "x\n"})
	fin := &fakeFinalizer{}
	fin.final = []rules.Match{{Path: spellNFD, Message: "file-level"}}
	r := mustRule(t, "no-hit", finding.SeverityError, []string{"**"}, fin)
	r.Allowlist = &config.Allowlist{
		File:    "allowlists/legacy.txt",
		Entries: []config.AllowlistEntry{{Path: spellNFC, Line: 5}},
	}
	got, err := engine.Run([]*config.Rule{r}, fset, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("findings = %+v", got)
	}
	if !got[0].Suppressed || got[0].SuppressedBy != "allowlist:allowlists/legacy.txt:5" {
		t.Fatalf("finalizer file-level finding not suppressed by the divergently spelled entry: %+v", got[0])
	}
}
