package scan

import (
	"strings"
)

// tree.go — a file's siblings, for the checker whose verdict on one file reads
// another. Split from scan.go, which the 750-line vendor cap bounds.

// Tree returns the walk that found f: every file it kept, whatever a later
// Restrict narrowed the run to. A variant answers for the file it was computed
// from. nil for a file made in memory, which has no siblings to read.
//
// It exists for the checker whose verdict on one file depends on another. A
// SQL query composed from a Go constant is the case: the constant is declared
// in a sibling file, or in another package of the module, more often than in
// the file that composes it, and a range scan must still read it when only
// the composing file changed.
func (f *File) Tree() *FileSet { return f.Raw().tree }

// WithContent returns an in-memory file at f's path holding content, in f's
// tree. It is how a checker judges f with some of it masked out: the content is
// the mask's, and Tree still answers with the walk, since the siblings are
// unchanged. Raw is the new file itself, not f — Raw serves a preprocessor
// variant's checker, and handing it f would hand back what the mask removed.
func (f *File) WithContent(content []byte) *File {
	m := NewMemFile(f.relPath, content)
	m.tree = f.Tree()
	return m
}

// File returns the file at the repo-relative, slash-separated path rel, or nil
// when the walk did not keep one there.
func (fs *FileSet) File(rel string) *File {
	fs.index()
	return fs.byPath[rel]
}

// Dir returns the files directly in the repo-relative, slash-separated
// directory dir ("." for the root), sorted by path.
func (fs *FileSet) Dir(dir string) []*File {
	fs.index()
	return fs.byDir[dir]
}

func (fs *FileSet) index() {
	fs.indexOnce.Do(func() {
		fs.byPath = make(map[string]*File, len(fs.Files))
		fs.byDir = map[string][]*File{}
		for _, f := range fs.Files {
			fs.byPath[f.relPath] = f
			dir := "."
			if i := strings.LastIndexByte(f.relPath, '/'); i >= 0 {
				dir = f.relPath[:i]
			}
			fs.byDir[dir] = append(fs.byDir[dir], f)
		}
	})
}

// Memo returns the value build makes for key, building it once per walk. It
// is how a checker keeps a fact derived from the whole tree — a package's
// constants — for every file of the run rather than re-deriving it per file.
// build runs outside any lock, so it may call Memo for another key; two
// callers racing on one key may both build, and the first stored wins.
func (fs *FileSet) Memo(key any, build func() any) any {
	fs.memoMu.Lock()
	v, ok := fs.memo[key]
	fs.memoMu.Unlock()
	if ok {
		return v
	}
	built := build()
	fs.memoMu.Lock()
	defer fs.memoMu.Unlock()
	if v, ok := fs.memo[key]; ok {
		return v
	}
	if fs.memo == nil {
		fs.memo = map[any]any{}
	}
	fs.memo[key] = built
	return built
}
