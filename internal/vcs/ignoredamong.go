package vcs

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// IgnoredAmong answers IgnoredUnder's question for the given paths only: it
// returns exactly the records IgnoredUnder(root) would return whose Path is one
// of paths or a directory above one, without listing the rest of the tree.
//
// WHY IT EXISTS (#126). A path-scoped command — check-content, rules-for — asks
// about one file, and every lookup it makes into the prune set (scan.NotScannedBy,
// the ghost overlay) is keyed by that file or one of its ancestor directories.
// IgnoredUnder answers by running `ls-files --others --ignored --directory` over
// the whole working tree: about 7 s on an 83k-file tree to resolve one path. The
// lookups never read any other key, so the answer restricted to those keys is
// the whole answer they can observe.
//
// HOW IT STAYS THE SAME ANSWER. IgnoredUnder's record for a path p exists when
// the ls-files walk lists p AND check-ignore confirms it. The confirmation is
// asked here in the same spelling (p/ for a directory, p otherwise), so it is
// the same answer. The listing is reproduced from what git's directory walk
// (dir.c, treat_path and treat_directory) decides at each level, read from the
// filesystem, the index and check-ignore:
//
//   - a component named .git is never listed or entered;
//   - a directory the index has entries under is entered and not listed;
//   - a directory the index records as a gitlink is neither;
//   - an untracked directory the walk excludes is listed as p/ and not entered
//     (the --directory collapse);
//   - an untracked directory the walk does not exclude is entered;
//   - a regular file or symlink is listed when it is untracked and excluded;
//     anything else (a fifo, a socket) is never listed;
//   - nothing below a non-directory is ever reached — a symlink to a directory
//     is a file to the walk.
//
// "The walk excludes d" is check-ignore's answer for d WITHOUT the slash: for a
// directory the index holds nothing under, git resolves its type with lstat and
// evaluates the same last_matching_pattern the walk does. The index facts come from ls-files
// --stage limited to the asked paths, compared case-insensitively where
// core.ignorecase is set, because the walk's index lookups fold case there.
//
// WHERE THE MODEL STOPS, THE WHOLE LISTING ANSWERS. Three shapes depend on more
// than the asked paths, and each falls back to IgnoredUnder's own two-step,
// restricted to the asked keys — the answer is then identical by construction:
//
//   - root is not the top level, so directories above root decide the walk;
//   - an untracked, un-excluded directory holds a .git entry: the walk treats a
//     nested repository as a leaf, and deciding whether it is one is git's
//     is_nonbare_repository_dir, not a stat;
//   - an untracked, un-excluded directory that check-ignore nonetheless confirms
//     as p/ (a pattern such as `p/*` matches the slash spelling): the listing
//     names p/ only when every path beneath it is ignored, which only a walk of
//     p can say. A directory the index records as a FILE lands here too when
//     the walk excludes it: check-ignore's tracked carve-out hides p from the
//     unslashed question but not from p/.
//
// The #175 cross-check runs per frame exactly as IgnoredUnder does, and the
// intersection commutes with the restriction: the keys are closed under taking
// a parent, so every record that could cover a kept path is itself a key.
func IgnoredAmong(root string, paths []string) ([]IgnoredPath, error) {
	keys := pathKeys(paths)
	if len(keys) == 0 {
		return nil, nil
	}
	ambient, err := ignoredAmong(root, keys, ambientIndex)
	if err != nil {
		return nil, err
	}
	if !indexMoved() {
		return ambient, nil
	}
	repoSide, err := ignoredAmong(root, keys, repositoryIndex)
	if err != nil {
		return nil, err
	}
	return keepAgreedPrunes(repoSide, ambient), nil
}

// pathKeys is every path given plus every directory above it, slash-separated,
// sorted and deduplicated. A parent is a prefix of its children and so sorts
// before them, which the root-down passes in modelIgnored rely on.
func pathKeys(paths []string) []string {
	seen := map[string]bool{}
	var keys []string
	for _, p := range paths {
		p = strings.Trim(filepath.ToSlash(p), "/")
		for p != "" && p != "." && !seen[p] {
			seen[p] = true
			keys = append(keys, p)
			p = path.Dir(p)
		}
	}
	sort.Strings(keys)
	return keys
}

// keepKeys restricts records to the given keys.
func keepKeys(recs []IgnoredPath, keys []string) []IgnoredPath {
	want := make(map[string]bool, len(keys))
	for _, k := range keys {
		want[k] = true
	}
	var kept []IgnoredPath
	for _, r := range recs {
		if want[r.Path] {
			kept = append(kept, r)
		}
	}
	return kept
}

// errNeedsWholeListing is the model's own signal that a key's answer depends on
// paths it was not asked about. It never leaves this file.
var errNeedsWholeListing = errors.New("answer depends on the whole listing")

func ignoredAmong(root string, keys []string, frame indexFrame) ([]IgnoredPath, error) {
	recs, err := modelIgnored(root, keys, frame)
	if errors.Is(err, errNeedsWholeListing) {
		all, err := ignoredUnder(root, frame)
		if err != nil {
			return nil, err
		}
		return keepKeys(all, keys), nil
	}
	return recs, err
}

// keyState is what the walk would find at one key.
type keyState struct {
	mode     fs.FileMode // lstat type bits; meaningful when exists
	exists   bool
	reached  bool // the walk visits this entry
	entered  bool // the walk descends into it
	gitName  bool // the entry is named .git, which the walk skips
	tracked  bool // the index holds this exact path
	gitlink  bool // ... as a gitlink
	hasUnder bool // the index holds a path below it
}

func (k *keyState) isDir() bool { return k.exists && k.mode.IsDir() }

// listable reports whether the walk can list a non-directory: a regular file or
// a symlink. dir.c's treat_path answers path_none for every other type.
func (k *keyState) listable() bool {
	return k.exists && (k.mode.IsRegular() || k.mode&fs.ModeSymlink != 0)
}

func modelIgnored(root string, keys []string, frame indexFrame) ([]IgnoredPath, error) {
	prefix, err := frame.git(root, "rev-parse", "--show-prefix")
	if err != nil {
		return nil, err
	}
	if strings.TrimSuffix(prefix, "\n") != "" {
		return nil, errNeedsWholeListing
	}
	fold, err := frame.ignoreCase(root)
	if err != nil {
		return nil, err
	}

	// Pass 1, the filesystem: which keys the walk can reach at all.
	states := make(map[string]*keyState, len(keys))
	var onDisk []string
	for _, k := range keys {
		st := &keyState{gitName: sameName(path.Base(k), ".git", fold)}
		states[k] = st
		if !parentEntered(states, k) {
			continue
		}
		fi, err := os.Lstat(filepath.Join(root, filepath.FromSlash(k)))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		st.exists, st.mode = true, fi.Mode().Type()
		st.reached = true
		onDisk = append(onDisk, k)
		// Provisional: the index and exclusion passes below can only narrow it.
		st.entered = st.isDir() && !st.gitName
	}
	if len(onDisk) == 0 {
		return nil, nil
	}

	// Pass 2, the index.
	if err := readIndexFacts(root, frame, onDisk, fold, states); err != nil {
		return nil, err
	}

	// Pass 3, check-ignore: every key that could still be listed or entered, in
	// both spellings a directory needs.
	var asks []string
	for _, k := range keys {
		st := states[k]
		if !st.reached || !parentEntered(states, k) || st.gitName {
			continue
		}
		switch {
		case st.isDir():
			// An index cannot hold both k and a path below it (git refuses the
			// directory/file conflict), so the order of these two tests is never
			// what decides.
			if st.hasUnder {
				continue
			}
			if st.gitlink {
				st.entered = false
				continue
			}
			asks = append(asks, k, k+"/")
		case st.listable() && !st.tracked:
			asks = append(asks, k)
		}
	}
	var verdicts []IgnoredPath
	if len(asks) > 0 {
		if verdicts, err = confirmIgnored(root, asks, frame); err != nil {
			return nil, err
		}
	}
	asFile := map[string]IgnoredPath{}
	asDir := map[string]IgnoredPath{}
	for _, v := range verdicts {
		if v.Dir {
			asDir[v.Path] = v
		} else {
			asFile[v.Path] = v
		}
	}

	// Pass 4, root-down: what the listing holds, confirmed.
	var recs []IgnoredPath
	for _, k := range keys {
		st := states[k]
		st.reached = st.reached && parentEntered(states, k)
		if !st.reached || st.gitName {
			st.entered = false
			continue
		}
		switch {
		case st.isDir():
			if st.gitlink || st.hasUnder {
				continue // entered when it has entries, never when a gitlink
			}
			_, excluded := asFile[k]
			confirmed, slashConfirmed := asDir[k]
			if excluded {
				// The collapse: listed as k/, never entered. check-ignore on k/
				// evaluates k as a directory with the same pattern lists, so it
				// confirms whenever the unslashed question did — no fixture can
				// make this arm fire. It stays because the answer it falls back
				// to is the whole listing's own, where a wrong model would prune.
				if !slashConfirmed {
					return nil, errNeedsWholeListing
				}
				recs = append(recs, confirmed)
				st.entered = false
				continue
			}
			if slashConfirmed {
				return nil, errNeedsWholeListing
			}
			if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(k), ".git")); err == nil {
				return nil, errNeedsWholeListing
			} else if !os.IsNotExist(err) {
				return nil, err
			}
		case st.listable() && !st.tracked:
			if v, ok := asFile[k]; ok {
				recs = append(recs, v)
			}
		}
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].Path < recs[j].Path })
	return recs, nil
}

// parentEntered reports whether the walk descends into k's parent. The root is
// always entered.
func parentEntered(states map[string]*keyState, k string) bool {
	parent := path.Dir(k)
	if parent == "." {
		return true
	}
	st := states[parent]
	return st != nil && st.entered
}

// readIndexFacts records, for each key on disk, whether the index holds it,
// whether as a gitlink, and whether it holds anything below it. The pathspecs
// are the keys themselves, so git lists only those subtrees; :(literal) keeps a
// name holding a glob character from matching anything else, and :(icase) is
// added where the walk folds case, so a case-variant entry is not missed.
func readIndexFacts(root string, frame indexFrame, keys []string, fold bool, states map[string]*keyState) error {
	magic := ":(literal)"
	if fold {
		magic = ":(literal,icase)"
	}
	args := []string{"ls-files", "-z", "--stage", "--"}
	byName := make(map[string]*keyState, len(keys))
	for _, k := range keys {
		args = append(args, magic+k)
		byName[foldName(k, fold)] = states[k]
	}
	out, err := frame.git(root, args...)
	if err != nil {
		return err
	}
	for _, rec := range strings.Split(out, "\x00") {
		// Each record: "<mode> <object> <stage>\t<path>".
		tab := strings.IndexByte(rec, '\t')
		if tab < 0 {
			continue
		}
		name := foldName(filepath.ToSlash(rec[tab+1:]), fold)
		if st := byName[name]; st != nil {
			st.tracked = true
			if strings.HasPrefix(rec, "160000 ") {
				st.gitlink = true
			}
		}
		for i := 0; i < len(name); i++ {
			if name[i] != '/' {
				continue
			}
			if st := byName[name[:i]]; st != nil {
				st.hasUnder = true
			}
		}
	}
	return nil
}

// ignoreCase is core.ignorecase as git resolves it for root; unset is false.
func (f indexFrame) ignoreCase(root string) (bool, error) {
	out, err := f.git(root, "config", "--type=bool", "--default=false", "--get", "core.ignorecase")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) == "true", nil
}

// foldName is name as the walk's index lookups compare it: ASCII case folded
// where core.ignorecase is set (git's strncasecmp), unchanged otherwise.
func foldName(name string, fold bool) string {
	if !fold {
		return name
	}
	b := []byte(name)
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

func sameName(a, b string, fold bool) bool {
	return foldName(a, fold) == foldName(b, fold)
}
