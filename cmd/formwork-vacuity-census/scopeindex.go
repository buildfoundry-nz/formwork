package main

import (
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// buildScopeIndex computes, for every rule, the ordered subset of files it
// applies to — the same result serialScopes/filesInScope would produce, but
// computed with the per-rule scans distributed across a worker pool instead
// of run one rule at a time. Each rule's own scan stays serial (so file
// order within a rule's result is preserved exactly), only the rules
// themselves are fanned out; this is what keeps a large PR's O(R×F)
// scope-membership check off the wall-clock critical path (#12419).
//
// #14891: the parallel pool alone stopped fitting the budget once the
// corpus reached ~2166 rules × ~21k files (46M glob evaluations — 11.07s
// on a 4-vCPU CI runner against the pinned 10s wall). Two semantics-
// preserving reductions, both held to byte-identical results by
// TestBuildScopeIndexMatchesSerialTrickyGlobs:
//
//   - Candidate narrowing (scopeBuckets below): each rule's file loop
//     runs over the sorted ranges carrying its include globs' literal
//     prefixes — a 6.5× reduction in candidate evaluations on the real
//     corpus.
//   - Scope-signature dedupe: rules with identical
//     include/exclude/except.paths globs produce identical scopes by
//     construction, so one bucketed scan serves the whole group (2167
//     rules → 1356 unique signatures on the real corpus). Members share
//     the one result slice; every downstream consumer is read-only.
func buildScopeIndex(rules []*config.Rule, files []*scan.File) map[string][]*scan.File {
	scopes := make(map[string][]*scan.File, len(rules))
	if len(rules) == 0 {
		return scopes
	}
	buckets := newScopeBuckets(files)
	groups := groupByScopeSignature(rules)

	var mu sync.Mutex
	var wg sync.WaitGroup

	workers := runtime.GOMAXPROCS(0)
	if workers > len(groups) {
		workers = len(groups)
	}
	if workers < 1 {
		workers = 1
	}

	jobs := make(chan []*config.Rule)
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for members := range jobs {
				matched := filesInScopeBucketed(members[0], files, buckets)
				mu.Lock()
				for _, r := range members {
					scopes[r.ID] = matched
				}
				mu.Unlock()
			}
		}()
	}
	for _, members := range groups {
		jobs <- members
	}
	close(jobs)
	wg.Wait()

	return scopes
}

// groupByScopeSignature buckets rules by their exact
// include/exclude/except.paths glob lists: identical globs make Applies
// an identical predicate, so the scope is computed once per signature.
func groupByScopeSignature(rules []*config.Rule) [][]*config.Rule {
	bySig := make(map[string]int, len(rules))
	var groups [][]*config.Rule
	for _, r := range rules {
		sig := scopeSignature(r)
		if i, ok := bySig[sig]; ok {
			groups[i] = append(groups[i], r)
			continue
		}
		bySig[sig] = len(groups)
		groups = append(groups, []*config.Rule{r})
	}
	return groups
}

func scopeSignature(r *config.Rule) string {
	var b strings.Builder
	for _, g := range r.Include() {
		b.WriteString(g)
		b.WriteByte(0)
	}
	b.WriteByte(1)
	for _, g := range r.Exclude() {
		b.WriteString(g)
		b.WriteByte(0)
	}
	b.WriteByte(1)
	for _, g := range r.ExceptPaths {
		b.WriteString(g)
		b.WriteByte(0)
	}
	return b.String()
}

// filesInScope is the serial per-rule full scan: every file in files, in
// order, that r.Applies to. This is the same union predicate every other
// scope check in this module uses (calibrate.go, main.go, relation.go) —
// the index narrows and parallelizes, never re-implements the match. It
// stays as the baseline the bucketed path is pinned equal to
// (serialScopes in scopeindex_test.go).
func filesInScope(r *config.Rule, files []*scan.File) []*scan.File {
	matched := make([]*scan.File, 0, len(files))
	for _, f := range files {
		if r.Applies(f.Path()) {
			matched = append(matched, f)
		}
	}
	return matched
}

// filesInScopeBucketed is filesInScope narrowed to the candidate ranges
// carrying the rule's include-glob literal prefixes: a glob match must
// begin with the glob's meta-free literal prefix, so files outside the
// union of those ranges cannot be in scope. Every candidate is still
// decided by r.Applies, in the walk's original order — byte-identical
// results, 6.5× fewer evaluations on the real corpus (#14891).
func filesInScopeBucketed(r *config.Rule, files []*scan.File, buckets scopeBuckets) []*scan.File {
	candidates := buckets.candidates(r.Include())
	matched := make([]*scan.File, 0, len(candidates))
	for _, idx := range candidates {
		f := files[idx]
		if r.Applies(f.Path()) {
			matched = append(matched, f)
		}
	}
	return matched
}

// scopeBuckets orders the scanned paths once so a rule's candidate files
// narrow to the sorted ranges carrying its include globs' literal
// prefixes. A glob match must begin with the glob's meta-free literal
// prefix (glob metacharacters match only what follows the literal), so
// files outside the union of those prefix ranges cannot be in scope and
// skipping them changes no result.
type scopeBuckets struct {
	paths []string // sorted file paths
	order []int    // order[i] = original walk index of paths[i]
}

func newScopeBuckets(files []*scan.File) scopeBuckets {
	b := scopeBuckets{paths: make([]string, len(files)), order: make([]int, len(files))}
	perm := make([]int, len(files))
	for i := range perm {
		perm[i] = i
	}
	sort.Slice(perm, func(i, j int) bool {
		pi, pj := files[perm[i]].Path(), files[perm[j]].Path()
		if pi != pj {
			return pi < pj
		}
		return perm[i] < perm[j]
	})
	for i, o := range perm {
		b.paths[i] = files[o].Path()
		b.order[i] = o
	}
	return b
}

// candidates returns the original walk indices of every file whose path
// carries some include glob's literal prefix, deduplicated and in
// ascending original order (so a rule's result preserves the walk's file
// order exactly).
func (b scopeBuckets) candidates(include []string) []int {
	ranges := make([][2]int, 0, len(include))
	for _, g := range include {
		pre := literalPrefix(g)
		lo := sort.SearchStrings(b.paths, pre)
		hi := len(b.paths)
		if up := prefixUpperBound(pre); up != "" {
			hi = sort.SearchStrings(b.paths, up)
		}
		if lo < hi {
			ranges = append(ranges, [2]int{lo, hi})
		}
	}
	if len(ranges) == 0 {
		return nil
	}
	sort.Slice(ranges, func(i, j int) bool { return ranges[i][0] < ranges[j][0] })
	out := make([]int, 0, len(ranges))
	lo, hi := ranges[0][0], ranges[0][1]
	flush := func() {
		for i := lo; i < hi; i++ {
			out = append(out, b.order[i])
		}
	}
	for _, r := range ranges[1:] {
		if r[0] <= hi {
			if r[1] > hi {
				hi = r[1]
			}
			continue
		}
		flush()
		lo, hi = r[0], r[1]
	}
	flush()
	sort.Ints(out)
	return out
}

// literalPrefix is the longest metacharacter-free prefix of a glob: every
// path the glob matches carries it verbatim. `\` is meta (it escapes the
// next byte), so the prefix stops before it too.
func literalPrefix(glob string) string {
	if i := strings.IndexAny(glob, `*?[{\`); i >= 0 {
		return glob[:i]
	}
	return glob
}

// prefixUpperBound returns the smallest string strictly greater than
// every string carrying prefix p — trailing 0xff bytes are carried over
// (`a\xff` → `b`), and an all-0xff prefix has no upper bound (empty
// string means "to the end of the sorted list").
func prefixUpperBound(p string) string {
	b := []byte(p)
	for i := len(b) - 1; i >= 0; i-- {
		if b[i] != 0xff {
			b[i]++
			return string(b[:i+1])
		}
	}
	return ""
}
