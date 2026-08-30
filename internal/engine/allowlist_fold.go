// allowlist_fold.go — the run-scoped state exemption suppression needs beyond
// the finding it is judging. Split from engine.go, which the 750-line vendor
// cap bounds; same package.
package engine

import (
	"sync"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// exemptions is what a run carries into suppression: the file set it actually
// walked, and the per-rule fold of allowlist entries onto the spellings that
// set carries. A finding names a path as a string, an allowlist entry names one
// as a string, and whether those two strings are the same FILE is a question
// only the filesystem can answer — so the walk has to reach the suppression
// seam for the question to be askable there at all.
//
// One value per engine.Run, threaded rather than package-level: two runs in one
// process walk different trees — internal/fixturetest/run.go:179 calls Run once
// per fixture, and internal/meta/prefilter.go calls it twice over one tree — and
// a shared value would answer one run's question with the other's files.
type exemptions struct {
	fset *scan.FileSet

	mu   sync.Mutex
	fold map[*config.Rule]map[string]config.AllowlistEntry
}

func newExemptions(fset *scan.FileSet) *exemptions {
	return &exemptions{fset: fset, fold: map[*config.Rule]map[string]config.AllowlistEntry{}}
}

// folded returns r's fold: walked path -> the allowlist entry that names that
// same file under a different spelling. A path with no divergently spelled entry
// is absent, and a rule with no fold at all yields an empty map.
//
// Built at most once per rule per run, on first ask, under the lock. Phase 1
// evaluates one rule across many files concurrently, so the lock is what keeps
// the build from happening once per worker; it is held across the build
// deliberately, since a second worker arriving mid-build wants that same answer
// and computing it twice buys nothing.
//
// LAZY, because the build is only ever worth its cost where it can change
// something. It is reached from suppressAllowlist's fallback, which runs only
// for a rule that HAS an allowlist and produced a finding no entry answered for
// exactly — so a rule with no allowlist never builds one, and a rule whose
// allowlist did its job by exact spelling never builds one either.
func (x *exemptions) folded(r *config.Rule) map[string]config.AllowlistEntry {
	x.mu.Lock()
	defer x.mu.Unlock()
	if m, ok := x.fold[r]; ok {
		return m
	}
	m := foldAllowlist(r, x.fset)
	x.fold[r] = m
	return m
}

// foldAllowlist resolves each allowlist entry onto the walked path that names
// the same file, for entries the walk spells differently from the entry.
//
// THE ORACLE IS THE FILESYSTEM, and it is ASKED rather than recomputed:
// scan.(*FileSet).Produced owns "did this scan read the file this path names",
// on device+inode identity with a non-ASCII gate on both sides, and a second
// copy of that oracle on this side of the package boundary is exactly the
// divergence #151 is about. Asked of a ONE-FILE set, because engine needs the
// narrower answer Produced's bool cannot carry over the whole set: not whether
// some walked file is that file, but which one — the attribution
// `allowlist:<file>:<line>` names a specific entry, and an entry resolved onto
// the wrong walked path would send an operator to the wrong line to remove an
// exemption.
//
// The full-set ask in front of that loop is a COST guard and nothing else, said
// plainly because no test can hold it: fset.Produced(e.Path) is exactly the
// disjunction the loop computes file by file, so it can never exclude a case the
// loop would have found, and mutating it away leaves this package green. What it
// buys is still real — for a non-ASCII entry naming a file this walk never
// produced (a stale entry, the common shape in a mature allowlist) the loop
// would Lstat the entry's own path once per walked file, and the gate answers
// that in one pass.
//
// Entries whose exact path the walk already carries are skipped outright: the
// exact arm in suppressAllowlist answers those, and folding them here could only
// disagree with it. First entry wins for a given walked path, which is the same
// order the exact arm resolves ties in.
func foldAllowlist(r *config.Rule, fset *scan.FileSet) map[string]config.AllowlistEntry {
	if r.Allowlist == nil || fset == nil {
		return nil
	}
	walked := make(map[string]bool, len(fset.Files))
	for _, f := range fset.Files {
		walked[f.Path()] = true
	}
	out := map[string]config.AllowlistEntry{}
	one := &scan.FileSet{Root: fset.Root, Files: make([]*scan.File, 1)}
	for _, e := range r.Allowlist.Entries {
		if e.Path == "" || walked[e.Path] || !fset.Produced(e.Path) {
			continue
		}
		for _, f := range fset.Files {
			if _, taken := out[f.Path()]; taken {
				continue
			}
			one.Files[0] = f
			if one.Produced(e.Path) {
				out[f.Path()] = e
				break
			}
		}
	}
	return out
}
