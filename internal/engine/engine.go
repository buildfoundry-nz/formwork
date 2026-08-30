// Package engine runs compiled rules over a FileSet: phase 1 fans files out
// to a worker pool, phase 2 fans finalizers out to a second one (spec §6). A
// crashed rule is an engine error, never a pass (spec §11).
package engine

import (
	"fmt"
	"runtime"
	"sync"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/finding"
	"github.com/buildfoundry-nz/formwork/internal/marker"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// heavyFinalizerWorkers bounds how many *process-bound* CostHeavy finalizers
// may be in flight at once (Dart/Flutter analyzer-class command rules, and
// CostHeavy checkers that do not implement ProcessBound). Independently of
// --workers. Downstream one run forked four resolved-AST Dart analyzers at
// 3.2-8.6 GB each, ~20 GB in total, hard-hanging a 24 GB machine twice in a
// day (#67).
//
// It is 2, not 1, and the number is measured, not guessed. Against an early
// production corpus (dc54207e9: 84 heavy rules, ~80 of them sub-second `go
// run` detectors, 2 the multi-GB analyzer class) fully serialising *all*
// heavy rules cost +48s wall. The corpus then grew to ~230 go run command
// rules plus 5 dart runs: putting them all at width 2 made the validating
// port's own guardrail suite a 17-minute `check --lane ci`. go/bash
// command and git-diff stay CostHeavy (--skip-escapes, fixture-exempt) but
// run at full --workers. Width 2 applies only to the analyzer class.
// TestRunBoundsHeavyFinalizersToTwo pins the bound;
// TestRunWideHeavyFinalizersUseFullWorkerWidth pins the cheap-command half.
//
// It is also the slot count of the machine-wide gate in heavygate.go (#81),
// which is what makes this number a ceiling for the machine rather than for
// one invocation: N parallel formwork processes cost what one run costs. The
// two readings are deliberately the same constant — see the heavyGateDir
// comment for why widening one alone would break that.
const heavyFinalizerWorkers = 2

// selfBounded is optionally implemented by a per-file Checker that takes its
// OWN concurrency limit inside CheckFile. sqlparse is the shipped one: it
// admits at most four callers to the WASM parser because each go-pgquery
// instance costs roughly 250 MB resident and the pool never gives the memory
// back (#83). Shaped after rules.ProcessBound, and engine-local because it
// decides exactly one thing — which half of the phase-1 partition below a rule
// is dispatched through.
//
// Deliberately a bool, not a width. sqlparse's Go AST walk over .go files runs
// OUTSIDE its parser bound and dominates the per-file cost on a source corpus,
// so sizing the bounded pool to the checker's own limit would starve that walk
// and make SQL-only runs slower than they are now. A width the engine did not
// honour would be an interface that lies, so none is offered.
type selfBounded interface {
	SelfBounded() bool
}

// selfBoundedOf reports whether c bounds its own concurrency inside CheckFile.
// Mirrors rules.ProcessBoundOf. The default is false: an ordinary checker
// blocks on nothing, so it belongs in the full-width half.
func selfBoundedOf(c rules.Checker) bool {
	if s, ok := c.(selfBounded); ok {
		return s.SelfBounded()
	}
	return false
}

// phase1Rule pairs a phase-1 rule with its index in the run's declaration
// order. The partition below splits the rules into two slices dispatched
// through two pools, and once a rule has been moved into a sub-slice its
// position in the original list is the only thing left that can rank an error
// from one pool against an error from the other.
//
// Phase 2 solves the same problem with the bare index of its `fins` slice
// (finErrIdx). Phase 1 needs a struct because the partition RE-ORDERS its
// rules, where fins is dispatched by index and never re-ordered.
type phase1Rule struct {
	idx  int
	rule *config.Rule
}

// earlierVisit reports whether the (file, rule) pair (aFile, aRule) is visited
// before (bFile, bRule) by a --workers 1 run: files in scan order, and within
// one file, rules in declaration order.
//
// That order is the whole specification of which engine error a run reports. A
// concurrent phase 1 discovers errors in arrival order, which is a coin flip
// between pools and between workers; reducing them with this comparison makes a
// run report exactly what the serial pass would have — which is also what
// --workers 1 still reports through the merged branch below.
//
// File position dominates rule position, and that asymmetry is load-bearing: a
// serial run finishes EVERY rule on the first file before it looks at the
// second, so a rule declared first but scoped to a later file is one the serial
// pass never reaches. Ranking by rule index alone — phase 2's mechanism, which
// is complete for finalizers because they have no files — would name it
// (TestRunPhase1ErrorIsTheOneTheSerialPassReports).
func earlierVisit(aFile, aRule, bFile, bRule int) bool {
	if aFile != bFile {
		return aFile < bFile
	}
	return aRule < bRule
}

// Run evaluates every rule against every in-scope file, then finalizers.
// workers <= 0 means GOMAXPROCS. Findings come back deterministically sorted
// and include suppressed findings (spec §5 exemptions): callers that gate on
// findings must filter with finding.Unsuppressed.
func Run(rls []*config.Rule, fset *scan.FileSet, workers int) ([]finding.Finding, error) {
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	// The walk reaches the suppression seam, because whether an allowlist entry
	// and a finding name the same file is a question about the filesystem.
	ex := newExemptions(fset)

	var (
		mu       sync.Mutex
		findings []finding.Finding
		firstErr error
		// Where firstErr came from, in visit order. Meaningless while firstErr
		// is nil, and only ever read under mu.
		firstErrFile, firstErrRule int
	)
	// runFilePool walks the whole file list with `width` workers, evaluating
	// only `rs` against each file. Two of these run concurrently over the SAME
	// list, which is safe because scan.File caches its content, lines and
	// preprocessor variants behind its own sync primitives.
	//
	// Jobs are file INDICES rather than files, and each rule carries its
	// declaration index, because those two numbers are the visit position
	// earlierVisit ranks engine errors by — the only thing that can order an
	// error from one half of the partition against one from the other.
	runFilePool := func(rs []phase1Rule, width int) {
		if len(rs) == 0 {
			return
		}
		jobs := make(chan int)
		var wg sync.WaitGroup
		for range width {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for fi := range jobs {
					f := fset.Files[fi]
					for _, pr := range rs {
						if !pr.rule.Applies(f.Path()) {
							continue
						}
						fds, err := evalFile(pr.rule, f, ex)
						mu.Lock()
						if err != nil {
							if firstErr == nil || earlierVisit(fi, pr.idx, firstErrFile, firstErrRule) {
								firstErr, firstErrFile, firstErrRule = err, fi, pr.idx
							}
						} else {
							findings = append(findings, fds...)
						}
						mu.Unlock()
					}
				}
			}()
		}
		for fi := range fset.Files {
			jobs <- fi
		}
		close(jobs)
		wg.Wait()
	}
	// Cost partitions phase 1 as well, and against a different resource from
	// phase 2's. A self-bounded checker takes its own admission INSIDE
	// CheckFile, and that admission is a BLOCKING acquire on the calling
	// goroutine: in one flat pool the worker waiting for a slot is parked —
	// still holding its file, taking nothing else off the queue — so a bound
	// meant for the WASM parser throttles every OTHER rule with it. Measured
	// at the shipped default (parser bound 4, --workers 12) over 200 .sql
	// files under sql/parses plus 4,000 .txt files under forbidden-pattern, a
	// mixed run cost sum(sql, txt) = 1.19s rather than max(sql, txt) = 0.67s
	// (#83 acceptance criterion 2, #315).
	//
	// Both halves get the FULL width, where phase 2 SHARES --workers between
	// its two heavy pools. The difference is the resource. Phase 2's halves
	// fork subprocesses, so two pools of width w really are 2w processes and
	// 2w times the RAM. These are goroutines: actual parallelism is capped by
	// GOMAXPROCS however many are runnable, and every file's content ends up
	// retained for the run either way, so the second pool costs scheduling,
	// not cores or footprint.
	//
	// Stated honestly, it is NOT free: a self-bounded checker also does work
	// outside its own bound — sqlparse's Go AST walk over .go files — so a
	// mixed run can have up to 2w goroutines wanting CPU. Sharing --workers
	// instead would fix that and cost more than it saves: the bounded half
	// would drop below the width it has today, making a SQL-heavy run slower
	// than before the partition to speed up a mixed one.
	//
	// That caveat is measured, not hypothetical. Same corpus, parser bound 4,
	// best-of-3 on a 12-core machine that was carrying other work: wiring the
	// shipped sqlparse rules into this half (internal/rules/sqlparse/parser.go
	// declares SelfBounded on all three) took a mixed run from 1.06s to 0.73s
	// at --workers 6 — two pools of 6, about one goroutine per core — and from
	// 0.93s to 0.68s at --workers 12 with the parser bound at 1, where the SQL
	// half is block-dominated. At bound 4 AND --workers 12 the same fix moved
	// 1.04s to 0.97s, inside the noise: the parking is gone in every row, and
	// what the two halves want once it is gone is CPU that was already spoken
	// for. sql-only and txt-only are unchanged throughout — the parser stays
	// bounded, the text rules keep their width. This partition removes
	// waiting; it does not manufacture cores.
	//
	// No rule can land in neither pool — an un-run rule would read as a pass
	// (TestRunPhase1PartitionRunsEveryRule).
	allRls := make([]phase1Rule, len(rls))
	var boundRls, wideRls []phase1Rule
	for i, r := range rls {
		pr := phase1Rule{idx: i, rule: r}
		allRls[i] = pr
		if selfBoundedOf(r.Checker) {
			boundRls = append(boundRls, pr)
		} else {
			wideRls = append(wideRls, pr)
		}
	}
	if workers <= 1 {
		// One file in flight at a time. Two concurrent pools of width 1 hand an
		// operator who asked for one worker two of them, on exactly the machine
		// they throttled to protect — the same merge phase 2 makes of its two
		// heavy halves below (#67). Dispatching allRls rather than the two
		// slices concatenated keeps a file's rules in declaration order.
		runFilePool(allRls, workers)
	} else {
		var phase1 sync.WaitGroup
		phase1.Add(2)
		go func() { defer phase1.Done(); runFilePool(wideRls, workers) }()
		go func() { defer phase1.Done(); runFilePool(boundRls, workers) }()
		phase1.Wait()
	}
	if firstErr != nil {
		return nil, firstErr
	}

	// Phase 2: finalizers (cross-file joins plus the command/git-diff escapes).
	// These run concurrently: a lane can hold well over a hundred command rules
	// that each shell out to an external script, so serial execution adds their
	// latencies together. Each finalizer is independent — it reads the shared
	// read-only FileSet and execs its own tool. Findings merge under the same
	// mutex and are sorted at the end, so the output stays deterministic.
	//
	// Concurrency is NOT uniform across them, and that asymmetry is the point:
	// phase-1 worker count is the right width for an in-process join and the
	// wrong one for a rule that forks a whole-tree analyzer (#67). See the cost
	// partition below.
	fins := make([]*config.Rule, 0, len(rls))
	for _, r := range rls {
		switch r.Checker.(type) {
		case rules.ErrFinalizer, rules.Finalizer:
			fins = append(fins, r)
		}
	}
	// finErrIdx keeps the reported error deterministic: whichever worker fails
	// first, the error surfaced is the one from the earliest rule in
	// declaration order, exactly as the serial pass reported it. It is indexed
	// against fins, so it stays declaration-ordered across the three pools below.
	var (
		finErr    error
		finErrIdx int
	)
	// Assigned below, once the cost partition has said whether this run has any
	// analyzer-class rules at all; nil until then and nil forever if it has
	// none, so a corpus without them creates no lock files and does no
	// filesystem work. runFinalizer only reads it from inside runPool, which
	// starts after the assignment.
	var gate *heavyGate
	runFinalizer := func(i int) {
		r := fins[i]
		// The machine-wide slot is taken here rather than around a whole pool,
		// because the resource being bounded is one analyzer subprocess, not
		// one pool: holding a slot for a run's entire heavy phase would let the
		// first process on the machine starve the rest for as long as its
		// slowest rule takes. Gating is decided from the rule, not from which
		// pool dispatched it — under --workers 1 the two heavy partitions merge
		// into one pool below, and the cheap go/bash half of that pool must not
		// be gated (#81).
		if gate != nil && rules.ProcessBoundOf(r.Checker) {
			release := gate.acquire()
			defer release()
		}
		var (
			matches []rules.Match
			err     error
		)
		switch fin := r.Checker.(type) {
		case rules.ErrFinalizer:
			matches, err = finalizeErr(r, fin, rules.FinalizeContext{Root: fset.Root})
		case rules.Finalizer:
			matches, err = finalize(r, fin)
		}
		mu.Lock()
		if err != nil {
			if finErr == nil || i < finErrIdx {
				finErr, finErrIdx = err, i
			}
		} else {
			for _, m := range matches {
				fd := toFinding(r, "", m)
				suppressAllowlist(r, &fd, ex)
				findings = append(findings, fd)
			}
		}
		mu.Unlock()
	}
	// Cost partitions the pool. Analyzer-class heavy rules (Dart/Flutter
	// command argv0, or CostHeavy checkers that do not declare otherwise)
	// are per-PROCESS RAM: downstream one run forked four resolved-AST
	// Dart analyzers at 3.2-8.6 GB each (#67). Those stay at
	// heavyFinalizerWorkers.
	//
	// Other CostHeavy rules (go run / bash command, git-diff) are still
	// skipped by --skip-escapes and fixture-exempt, but they are not
	// multi-GB: putting ~230 of them in the width-2 analyzer pool is what
	// stretched the validating port's guardrail suite to 17
	// minutes. They run concurrently with the analyzer pool, sharing
	// --workers so a 4 vCPU runner is not 2 Dart + 4 go run at once.
	//
	// Anything not CostHeavy runs full-width. No finalizer can land in
	// neither pool (an un-run rule would read as a pass).
	var fastIdx, wideHeavyIdx, boundHeavyIdx []int
	for i, r := range fins {
		if r.Cost() != rules.CostHeavy {
			fastIdx = append(fastIdx, i)
			continue
		}
		if rules.ProcessBoundOf(r.Checker) {
			boundHeavyIdx = append(boundHeavyIdx, i)
		} else {
			wideHeavyIdx = append(wideHeavyIdx, i)
		}
	}
	if len(boundHeavyIdx) > 0 {
		gate = &heavyGate{
			dir:   heavyGateDir,
			slots: heavyFinalizerWorkers,
			wait:  heavyGateWait,
			poll:  heavyGatePoll,
			warn:  heavyGateWarn,
		}
	}
	runPool := func(idx []int, width int) {
		if len(idx) == 0 {
			return
		}
		jobs := make(chan int)
		var wg sync.WaitGroup
		for range min(width, len(idx)) {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := range jobs {
					runFinalizer(i)
				}
			}()
		}
		for _, i := range idx {
			jobs <- i
		}
		close(jobs)
		wg.Wait()
	}
	boundW, wideW := heavyPoolWidths(workers, len(boundHeavyIdx), len(wideHeavyIdx))
	var finWg sync.WaitGroup
	if workers <= 1 {
		// One subprocess at a time. Two concurrent heavy pools of width 1
		// would oversubscribe --workers 1 (#67).
		allHeavy := append(append([]int{}, wideHeavyIdx...), boundHeavyIdx...)
		finWg.Add(2)
		go func() { defer finWg.Done(); runPool(fastIdx, workers) }()
		go func() { defer finWg.Done(); runPool(allHeavy, workers) }()
	} else {
		finWg.Add(3)
		go func() { defer finWg.Done(); runPool(fastIdx, workers) }()
		go func() { defer finWg.Done(); runPool(wideHeavyIdx, wideW) }()
		go func() { defer finWg.Done(); runPool(boundHeavyIdx, boundW) }()
	}
	finWg.Wait()
	if finErr != nil {
		return nil, finErr
	}

	finding.Sort(findings)
	return findings, nil
}

// heavyPoolWidths shares --workers across the analyzer-class pool and the
// cheap-command pool. Both are subprocesses; running width-2 Dart next to
// GOMAXPROCS go run oversubscribed a 4 vCPU runner on the validating port
// and pushed a scope-membership rule over its 10s budget.
// Combined in-flight heavies must not exceed workers.
// Bound still caps at heavyFinalizerWorkers. With no analyzer-class rules,
// cheap commands keep the full width (TestRunWideHeavyFinalizersUseFullWorkerWidth).
func heavyPoolWidths(workers, nBound, nWide int) (boundW, wideW int) {
	if nBound == 0 {
		return 0, workers
	}
	if nWide == 0 {
		return min(workers, heavyFinalizerWorkers), 0
	}
	boundW = min(nBound, min(workers, heavyFinalizerWorkers))
	wideW = workers - boundW
	if wideW < 1 {
		wideW = 1
		boundW = max(1, workers-1)
	}
	return boundW, wideW
}

func toFinding(r *config.Rule, fallbackPath string, m rules.Match) finding.Finding {
	path := m.Path
	if path == "" {
		path = fallbackPath
	}
	return finding.Finding{
		RuleID:   r.ID,
		Severity: r.Severity,
		Path:     path,
		Line:     m.Line,
		Message:  m.Message,
	}
}

// evalFile runs one rule over one file: picks the rule's preprocess variant,
// checks it, and applies exemption suppression against the raw file (markers
// live in comments, which preprocessors erase — phase-3a design §3).
func evalFile(r *config.Rule, f *scan.File, ex *exemptions) ([]finding.Finding, error) {
	work, err := f.Variant(r.Preprocess)
	if err != nil {
		return nil, fmt.Errorf("rule %s: %s: %w", r.ID, f.Path(), err)
	}
	matches, err := checkFile(r, work)
	if err != nil {
		return nil, err
	}
	out := make([]finding.Finding, 0, len(matches))
	for _, m := range matches {
		fd := toFinding(r, f.Path(), m)
		if err := suppress(r, f, &fd, ex); err != nil {
			return nil, err
		}
		out = append(out, fd)
	}
	return out, nil
}

// suppress marks fd Suppressed when the rule's inline marker (same raw line,
// reason mandatory — internal/marker owns the grammar) or allowlist (exact
// path) exempts it. Scope-level findings (no path) are never exemptable.
func suppress(r *config.Rule, raw *scan.File, fd *finding.Finding, ex *exemptions) error {
	if fd.Path == "" {
		return nil
	}
	if r.Marker && fd.Line > 0 && fd.Path == raw.Path() {
		lines, err := raw.Lines()
		if err != nil {
			return fmt.Errorf("rule %s: marker scan of %s: %w", r.ID, raw.Path(), err)
		}
		if fd.Line <= len(lines) && marker.Classify(lines[fd.Line-1], r.ID) == marker.Reasoned {
			fd.Suppressed = true
			fd.SuppressedBy = "marker"
			return nil
		}
	}
	suppressAllowlist(r, fd, ex)
	return nil
}

// suppressAllowlist applies allowlist suppression only; used directly for
// finalizer findings, where there is no per-file raw content to marker-scan.
//
// Two arms, in this order and not the other. The EXACT arm is the contract —
// an allowlist entry is a path, matched as written — and it stays first and
// untouched, because it is the only arm that can answer for a finding whose
// path this run's FileSet does not hold at all: a finalizer is free to report a
// path it synthesized, and folding first would have quietly stopped suppressing
// those.
//
// The FOLD arm is a fallback for the one case exact equality gets wrong: two
// byte sequences that name one file. On macOS/APFS an editor saves the entry
// NFC and readdir hands the walk the NFD bytes, so `e.Path == fd.Path` is false
// for a file the operator plainly exempted — and the failure is silent, because
// a finding that is merely NOT suppressed is indistinguishable from one nobody
// exempted (#308's class at this seam). exemptions.folded owns the
// resolution and asks scan.(*FileSet).Produced for it; the entry it returns is
// the one attributed, so the operator is sent to the line they actually wrote.
func suppressAllowlist(r *config.Rule, fd *finding.Finding, ex *exemptions) {
	if fd.Suppressed || fd.Path == "" || r.Allowlist == nil {
		return
	}
	for _, e := range r.Allowlist.Entries {
		if e.Path == fd.Path {
			allowlisted(fd, r.Allowlist, e)
			return
		}
	}
	if e, ok := ex.folded(r)[fd.Path]; ok {
		allowlisted(fd, r.Allowlist, e)
	}
}

// allowlisted marks fd exempt and attributes it to the entry that answered, so
// both arms above render one attribution rather than two spellings of it.
func allowlisted(fd *finding.Finding, al *config.Allowlist, e config.AllowlistEntry) {
	fd.Suppressed = true
	fd.SuppressedBy = fmt.Sprintf("allowlist:%s:%d", al.File, e.Line)
}

func checkFile(r *config.Rule, f *scan.File) (matches []rules.Match, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("rule %s panicked on %s: %v", r.ID, f.Path(), p)
		}
	}()
	matches, err = r.Checker.CheckFile(f)
	if err != nil {
		err = fmt.Errorf("rule %s: checking %s: %w", r.ID, f.Path(), err)
	}
	return matches, err
}

func finalize(r *config.Rule, fin rules.Finalizer) (matches []rules.Match, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("rule %s panicked in finalize: %v", r.ID, p)
		}
	}()
	return fin.Finalize(), nil
}

// finalizeErr runs an ErrFinalizer, converting a panic or a returned error
// into an engine error (exit 2) so an external-tool rule that cannot run
// never reads as a pass (spec §11).
func finalizeErr(r *config.Rule, fin rules.ErrFinalizer, ctx rules.FinalizeContext) (matches []rules.Match, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("rule %s panicked in finalize: %v", r.ID, p)
		}
	}()
	matches, err = fin.FinalizeErr(ctx)
	if err != nil {
		return nil, fmt.Errorf("rule %s: %w", r.ID, err)
	}
	return matches, nil
}
