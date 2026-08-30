// restrict.go — the file-set intersection: which walked files are the ones a
// caller's path list named, including where the two spell the same filename
// with different bytes (#98). Split from scan.go, which the 750-line vendor cap
// bounds; same package.
package scan

import (
	"os"
	"path/filepath"
	"sort"
)

// Restrict returns a new FileSet containing only files whose path is in allow
// (slash-separated, root-relative). Order is preserved. Used by file-set modes
// (--staged/--range) to scan only the changed files.
//
// Matching is by exact path, with one narrow exception the FILESYSTEM decides
// rather than this package: where a caller's path and a walked path are two
// spellings of one filename — the macOS NFC/NFD divergence of #98 — the file is
// matched by identity. foldSpellings below owns that, and owns why it cannot
// widen anything else.
//
// A path in allow that this FileSet does not carry is simply absent from the
// result, and that silence is NOT a feature — it is how a file staged and then
// deleted from the worktree passed every rule at exit 0 (#158). This function
// cannot do better: it is handed a set of paths with no record of why any of
// them is missing, and Ignored is not carried onto the result because the
// restricted set is the engine's input, not a census. Accounting for the
// difference is therefore the CALLER's obligation; internal/cli's
// requestedButAbsent is where it is discharged, and its caller exits 2 rather
// than let an unproduced path read as a checked one.
func (fs *FileSet) Restrict(allow map[string]bool) *FileSet {
	out := &FileSet{Root: fs.Root}
	var unmatched []*File
	served := map[string]bool{}
	for _, f := range fs.Files {
		if allow[f.Path()] {
			out.Files = append(out.Files, f)
			served[f.Path()] = true
			continue
		}
		if hasNonASCII(f.Path()) {
			unmatched = append(unmatched, f)
		}
	}
	if folded := fs.foldSpellings(allow, served, unmatched); len(folded) > 0 {
		out.Files = out.Files[:0]
		for _, f := range fs.Files {
			if allow[f.Path()] || folded[f] {
				out.Files = append(out.Files, f)
			}
		}
	}
	return out
}

// hasNonASCII reports whether s carries a byte outside ASCII. It is the gate on
// the fold below, and it is exact rather than conservative: normalization only
// ever moves non-ASCII characters, and the NFD of an ASCII character is that
// same ASCII character — so two spellings that differ by normalization BOTH
// carry a non-ASCII byte. An all-ASCII path therefore has no divergent
// spelling to look for, and an all-ASCII repository never reaches the fold at
// all.
//
// It is also the gate that keeps IDENTITY from widening past normalization. A
// hard link is two names for one inode with no normalization anywhere in it, so
// without this an on-disk allow path the walk had pruned could claim a walked
// file the caller never named.
//
// THE TWO CALL SITES ARE PINNED SEPARATELY, because they filter opposite sides
// of the intersection and either one alone is enough to empty the fold: the
// walked-side gate in Restrict decides which walked files become candidates,
// the requested-side gate in foldSpellings decides which allow paths go
// looking. TestRestrictDoesNotFoldAnASCIIPathOntoAHardLink is an all-ASCII
// fixture, so it is short-circuited by the walked-side gate and kills only the
// SIMULTANEOUS removal; it was for a while the only test here, and #333 is what
// measured the cost — with it alone, either half could be deleted and the whole
// module stayed green, the requested-side deletion serving a walked file the
// caller never named. The two mixed-alphabet hard-link fixtures beside it now
// kill each half on its own.
func hasNonASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return true
		}
	}
	return false
}

// foldSpellings answers #98: which walk files are the ones git named under a
// DIFFERENT BYTE SEQUENCE for the same filename. On macOS git reports NFC
// (core.precomposeunicode, default true) while readdir returns whatever is on
// disk — NFD for a file created decomposed — so the exact-equality intersection
// above misses, and the file drops out of the changed set AND out of the
// tracked set that whole-tree invariants and armed scope floors are measured
// over. That second arm carries no requestedButAbsent accounting, so it was a
// silent exit 0.
//
// THE ORACLE IS THE FILESYSTEM, NOT A UNICODE TABLE. os.SameFile compares the
// device and inode the two spellings resolve to, so a fold happens only where
// the two names ARE one file. That buys three things a normalization fold
// bought at the cost of a golang.org/x/text dependency would not:
//
//   - A repository legitimately carrying BOTH spellings is untouched. Such a
//     repository exists only on a normalization-SENSITIVE filesystem, where git
//     reports both names as they are on disk, both match exactly in the loop
//     above, and neither is ever a candidate here.
//   - No table, no dependency, no spec-level decision about one.
//   - It cannot invent a match: same device and inode is identity, not
//     similarity.
//
// The narrowing is deliberate at every step. Only allow paths NOTHING matched
// exactly are looked up, so an exactly-matched file is never re-served under a
// second spelling; only non-ASCII paths are considered, so no ASCII verdict
// anywhere can move; a path that is not on disk stays absent, which is what
// requestedButAbsent (#158) reads to refuse at exit 2; and each requested path
// takes AT MOST ONE file, so a hard-linked alias sharing an inode cannot smuggle
// a second file in behind the first. Candidates are consumed in fs.Files order,
// which is sorted, so the choice is deterministic.
//
// LSTAT, NOT STAT, ON BOTH SIDES, and that is the narrowing #333 stopped one
// mechanism short of. os.Stat FOLLOWS, so a requested spelling that is a
// SYMLINK resolved to its target's inode and os.SameFile then agreed with a
// walked file the caller never named: measured on this tree,
// Restrict({vendor/aperçu.ts}) returned [naïve.ts]. The hasNonASCII gate
// cannot stop it — the link's own name is non-ASCII, so it passes both call
// sites legitimately — and unlike #333's hard link, which its record scoped to
// "an out-of-band hard link git itself never creates", git tracks a symlink as
// mode 120000 and hands it to allow on every ordinary --staged, --range and
// trackedFileSet run. trackedFileSet passes the ENTIRE tracked list as allow, so
// a tracked symlink pointing at an UNTRACKED walked file folded that file into
// the tracked set, which is #23's asymmetry — an untracked file must not SATISFY
// an armed scope floor — with a pointer as the mechanism.
//
// A symlink is judged by the link and never by its target is the rule the rest
// of this engine already keeps: Produced Lstats and says so, internal/cli's
// requestedButAbsent Lstats and says so, and the walk classifies from the
// directory entry. This seam was the one that followed, which also made Restrict
// and Produced — the two halves of ONE accounting — disagree about the same
// path, so #308's caller could be told a file was not produced while this
// function had served it.
//
// On the WALKED side the choice is not load-bearing and is made anyway, for the
// reason Produced records: WalkWith appends only regular files, where Lstat and
// Stat agree. One spelling of the rule in both halves is worth more than a
// second reader working out why the two differ.
//
// The NFC/NFD fold is untouched by it. Both spellings name the same DIRECTORY
// ENTRY, and Lstat resolves the final component through the same
// normalization-insensitive lookup Stat does; the entry is a regular file, so
// the two calls return the same inode. TestRestrictMatchesNormalizationDivergent
// Spelling is what holds that rather than this paragraph.
//
// Cost in the ordinary case is nothing: with no unmatched non-ASCII file on
// either side the function returns before making a single stat call.
func (fs *FileSet) foldSpellings(allow, served map[string]bool, unmatched []*File) map[*File]bool {
	if len(unmatched) == 0 {
		return nil
	}
	var wanted []string
	for p := range allow {
		if !served[p] && hasNonASCII(p) {
			wanted = append(wanted, p)
		}
	}
	if len(wanted) == 0 {
		return nil
	}
	// Sorted only so a debugging read of this loop is reproducible; the result
	// is order-independent, since each file is claimed by at most one path and
	// each path claims at most one file.
	sort.Strings(wanted)
	folded := map[*File]bool{}
	for _, p := range wanted {
		want, err := os.Lstat(filepath.Join(fs.Root, filepath.FromSlash(p)))
		if err != nil {
			continue // not on disk under this spelling: genuinely absent
		}
		for _, f := range unmatched {
			if folded[f] {
				continue
			}
			got, err := os.Lstat(f.absPath)
			if err != nil || !os.SameFile(want, got) {
				continue
			}
			folded[f] = true
			break
		}
	}
	return folded
}

// Produced reports whether this FileSet is the scan OF THE FILE the caller's
// path names — including where the walk and the caller spell that one filename
// with different bytes. It is the fold above asked as a question, for the
// caller whose obligation Restrict's own doc names.
//
// It exists because #98's fix stopped at Restrict. internal/cli's
// requestedButAbsent, which must leave no requested path unaccounted for,
// rebuilds `present` as an exact string map over the restricted set; git hands
// it the NFC spelling and the walk carries the NFD one, so a file the fold DID
// produce read as unproduced. Measured on macOS/APFS at 887acefa with the real
// binary: a clean NFD-named file exits 2 under --staged and --range with
// "present in the working tree but not produced by the scan under this
// spelling" — both halves of that sentence false — with no cure line, and a
// real `git commit` through formwork's own installed pre-commit hook is refused
// (#308).
//
// THE ORACLE IS THE FILESYSTEM, exactly as in foldSpellings: same device and
// inode is identity, not similarity, and no unicode table or dependency is
// involved. The narrowing is the same too, and for the same reasons — only
// non-ASCII spellings, on EITHER side, are ever folded, so no ASCII verdict can
// move and a pruned hard link cannot be answered for by a walked file that
// merely shares its inode (#333).
//
// A path this FileSet does not carry under ANY spelling answers false, and that
// includes one git named that the worktree no longer has. That answer is what
// #158's accounting reads to refuse at exit 2, so guessing true here would turn
// a loud refusal into the false pass #158 closed.
//
// LSTAT, NOT STAT, for the requested spelling: a symlink is judged by the link
// and never by its target, so a symlink pointing AT a walked file is not that
// file. requestedButAbsent Lstats for the same reason and says so. On the
// walked side the choice is not load-bearing — WalkWith appends only regular
// files, where the two agree.
//
// Cost is one pass over Files at most, and for an ASCII path no stat call at
// all — it returns at the gate. The fold's stats are reached only where a
// non-ASCII spelling went genuinely unmatched.
func (fs *FileSet) Produced(p string) bool {
	for _, f := range fs.Files {
		if f.Path() == p {
			return true
		}
	}
	if !hasNonASCII(p) {
		return false
	}
	want, err := os.Lstat(filepath.Join(fs.Root, filepath.FromSlash(p)))
	if err != nil {
		return false // not on disk under this spelling: genuinely absent
	}
	for _, f := range fs.Files {
		if !hasNonASCII(f.Path()) {
			continue
		}
		got, err := os.Lstat(f.absPath)
		if err != nil || !os.SameFile(want, got) {
			continue
		}
		return true
	}
	return false
}
