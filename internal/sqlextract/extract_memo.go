package sqlextract

import (
	"bytes"
	"slices"
	"sync"

	"github.com/buildfoundry-nz/formwork/internal/scan"
)

type extractionKey struct {
	path        string
	reassembled bool
}

type extractionEntry struct {
	once       sync.Once
	candidates []Candidate
	sites      []Site
	err        error
}

// memoExtraction shares a walk's immutable file extraction across SQL rules.
// A masked file still has the original Tree, so path alone cannot identify its
// source: only the canonical walk content is retained. Every variant is read
// normally and left out of the memo, keeping mutation-probe results from growing
// the cache. Entries live with the walk, never in a process-global registry.
func memoExtraction(tree *scan.FileSet, path string, src []byte, reassembled bool, extract func() ([]Candidate, []Site, error)) ([]Candidate, []Site, error) {
	if tree == nil {
		return extract()
	}
	f := tree.File(path)
	if f == nil {
		return extract()
	}
	canonical, err := f.Content()
	if err != nil || !bytes.Equal(src, canonical) {
		return extract()
	}
	entry := tree.Memo(extractionKey{path, reassembled}, func() any { return &extractionEntry{} }).(*extractionEntry)
	// FileSet.Memo may race its builder. It only constructs the entry here;
	// the winner's Once owns the expensive extraction, including any error.
	entry.once.Do(func() { entry.candidates, entry.sites, entry.err = extract() })
	return slices.Clone(entry.candidates), slices.Clone(entry.sites), entry.err
}
