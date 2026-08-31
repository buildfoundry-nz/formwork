package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/engine"
	"github.com/buildfoundry-nz/formwork/internal/finding"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"gopkg.in/yaml.v3"
)

const (
	class1     = "class1"
	class1Glob = "class1-glob"
	class2     = "class2"
	class3     = "class3"
	// classNew is the question one level up from the other three: not "is this
	// rule vacuous?" but "was that question even asked?". It carries only rules
	// the change under judgement ADDS — see newrules.go (#15837).
	classNew = "class-new"
)

var classTitle = map[string]string{
	classNew:   "class N — a rule ADDED by this change that the census cannot decide",
	class1:     "class 1 — scope matches no file",
	class1Glob: "class 1 — scope.include glob integrity (per-glob / inventory)",
	class2:     "class 2 — the evidence is not the invariant",
	class3:     "class 3 — the fixture set, or a fixture, no longer discriminates",
}

// verdict is one census finding against a rule.
//
// gating is retained as a field, and is now true on every verdict the census
// can emit (#12178). It used to separate "this rule cannot fail" from "this
// rule is insensitive", and six classes lived on the second side: dead
// scope.exclude and except.paths globs, comment-plane-satisfiable rules,
// diffuse evidence, absence-only fixtures and rules with no fixture at all —
// ~970 instances that the closing "OK: every rule can fail" was computed
// without. A detected class that does not gate is credited as coverage while
// enforcing nothing, which is the same failure the census exists to catch, one
// level up. The field stays so a future verdict cannot be added as
// non-gating by accident: it must be written, and reviewed, as a deliberate
// false.
type verdict struct {
	class    string
	code     string
	why      string
	evidence []string
	gating   bool
}

// row is one rule's full classification.
type row struct {
	id, typ string
	scopeN  int

	existenceObligation bool
	relationObligation  bool
	witnesses           []string

	hasFixtures          bool
	fireCount, passCount int
	// fireInScope is how many files across every fire fixture the rule's scope
	// actually matches. Zero means every fire fixture fires on an empty scope.
	fireInScope int

	verdicts []verdict
}

// ruleMeta is the params metadata the compiled checker does not expose but the
// class-2 population depends on: whether the rule's obligation is existence.
type ruleMeta struct {
	mode string // required-pattern: exists | every-file
	op   string // pattern-count: at-least | at-most | exactly
	n    int

	// set-relation: the two sides' file globs and the relation between them.
	// The #12180 probe blanks the REQUIRED side and asks whether the relation
	// still holds — which is only a question when b IS the required side and
	// blanking it leaves the constrained side standing. See relation.go.
	// EMPTY-SIDE (V4) re-extracts each side's set with pattern/group so a
	// live equal|subset rule whose either side is cardinality 0 is gated.
	aFiles      []string
	bFiles      []string
	aPattern    string
	bPattern    string
	aGroup      int
	bGroup      int
	aPreprocess string
	bPreprocess string
	relation    string
	// pair-consistency: the trigger pattern. The #12181 probe asks whether it
	// still matches anything at all — a trigger that matches nothing obliges
	// nothing, so the `requires` half is never asked for. also_present and
	// where are needed so DEAD-TRIGGER uses the same unit semantics as the
	// engine (I17/I18): trigger∧also_present under same-func spans.
	trigger     string
	alsoPresent string
	where       string
}

// loadRuleMeta re-reads .formwork/rules/*.yaml for the params fields
// (mode/op/n) that config.Rule keeps private. Matching is never done here —
// only metadata is read; every verdict below is still rendered by the engine.
func loadRuleMeta(root string) (map[string]ruleMeta, error) {
	files, err := filepath.Glob(filepath.Join(root, ".formwork", "rules", "*.yaml"))
	if err != nil {
		return nil, err
	}
	out := map[string]ruleMeta{}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		var doc struct {
			Rules []struct {
				ID     string `yaml:"id"`
				Params struct {
					Mode string `yaml:"mode"`
					Op   string `yaml:"op"`
					N    int    `yaml:"n"`
					A    struct {
						Files      []string `yaml:"files"`
						Pattern    string   `yaml:"pattern"`
						Group      *int     `yaml:"group"`
						Preprocess string   `yaml:"preprocess"`
					} `yaml:"a"`
					B struct {
						Files      []string `yaml:"files"`
						Pattern    string   `yaml:"pattern"`
						Group      *int     `yaml:"group"`
						Preprocess string   `yaml:"preprocess"`
					} `yaml:"b"`
					Relation    string `yaml:"relation"`
					Trigger     string `yaml:"trigger"`
					AlsoPresent string `yaml:"also_present"`
					Where       string `yaml:"where"`
				} `yaml:"params"`
			} `yaml:"rules"`
		}
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		for _, r := range doc.Rules {
			aGroup, bGroup := 1, 1
			if r.Params.A.Group != nil {
				aGroup = *r.Params.A.Group
			}
			if r.Params.B.Group != nil {
				bGroup = *r.Params.B.Group
			}
			out[r.ID] = ruleMeta{
				mode: r.Params.Mode, op: r.Params.Op, n: r.Params.N,
				aFiles: r.Params.A.Files, bFiles: r.Params.B.Files,
				aPattern: r.Params.A.Pattern, bPattern: r.Params.B.Pattern,
				aGroup: aGroup, bGroup: bGroup,
				aPreprocess: r.Params.A.Preprocess, bPreprocess: r.Params.B.Preprocess,
				relation: r.Params.Relation, trigger: r.Params.Trigger,
				alsoPresent: r.Params.AlsoPresent, where: r.Params.Where,
			}
		}
	}
	return out, nil
}

// isExistenceObligation reports whether satisfying the rule requires evidence to
// EXIST somewhere in scope — the precondition for the WITNESS probes, which ask
// "which file carries the evidence" and need single-file satisfaction to mean
// something. A ceiling (pattern-count at-most) and required-pattern every-file
// are excluded: deleting a file satisfies those, so a single-file pass would not
// mean the file carries the evidence.
func isExistenceObligation(r *config.Rule, m ruleMeta) bool {
	if rules.CostOf(r.Checker) == rules.CostHeavy {
		return false
	}
	switch r.Type {
	case "required-pattern":
		return m.mode == "exists"
	case "pattern-count":
		return m.op == "at-least" && m.n > 0
	}
	return false
}

// lockdownSynthDirRel is the tree the heavy-rule fixture exemption defers to.
const lockdownSynthDirRel = "api-factory/internal/lockdowntests"

// lockdownSynthWitnesses returns the set of rule ids some file under
// lockdownSynthDirRel both NAMES and backs with a test function.
//
// Both halves are load-bearing. Naming alone is what the exemption assumed, and
// it is not enough: the since-deleted synth_form_coverage_markers_test.go named
// its subjects in twelve lines of `// FORM(...)` comments under a lockdown
// build tag, with no code at all (#12238, which also added the
// synth-form-marker-colocation lockdown against that shape returning). A file
// with no `func Test` runs nothing, so it can witness nothing, however many
// rule ids it mentions.
func lockdownSynthWitnesses(root string) (map[string]bool, error) {
	entries, err := os.ReadDir(filepath.Join(root, lockdownSynthDirRel))
	if os.IsNotExist(err) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(root, lockdownSynthDirRel, e.Name()))
		if err != nil {
			return nil, err
		}
		text := string(body)
		if !testFuncDecl.MatchString(text) {
			continue
		}
		for _, id := range ruleIDToken.FindAllString(text, -1) {
			out[id] = true
		}
	}
	return out, nil
}

var (
	// testFuncDecl matches a Go test function declaration at the start of a line.
	testFuncDecl = regexp.MustCompile(`(?m)^func Test\w*\(`)
	// ruleIDToken matches a formwork rule id shape: lowercase words joined by
	// hyphens. Over-matching is harmless — a witness set is only ever consulted
	// for ids that exist — while under-matching would gate a witnessed rule.
	ruleIDToken = regexp.MustCompile(`[a-z0-9]+(?:-[a-z0-9]+)+`)
)

// isRelationObligation reports whether the rule's obligation is a RELATION —
// set-relation (every element of one side must appear on the other) or
// pair-consistency (a trigger requires a companion). #12178 defect 1: these
// were outside the class-2 population entirely, so no probe ever ran on them,
// and #12180 / #12181 then found live vacuity in exactly those two types.
//
// They are not admitted to the witness probes, which would misread them — a
// relation is satisfied by an EMPTY tree, so "this file satisfies the rule
// alone" says nothing about where the evidence lives. Each type gets the probe
// that is valid for it instead (relationVerdicts below).
func isRelationObligation(r *config.Rule) bool {
	if rules.CostOf(r.Checker) == rules.CostHeavy {
		return false
	}
	return r.Type == "set-relation" || r.Type == "pair-consistency"
}

// declared is every func/method name in the tree, built once by the caller: the
// symbol-anchor probes need it to tell a DELETED subject from one that is merely
// uncalled right now, and the summary needs the same set to count the anchors
// this instrument cannot decide (symbol.go).
func classify(cfg *config.Config, root string, fset *scan.FileSet, scopes map[string][]*scan.File, meta map[string]ruleMeta, gm *globMeasure, declared map[string]bool) ([]row, error) {
	synths, err := lockdownSynthWitnesses(root)
	if err != nil {
		return nil, err
	}
	// Path index over the engine's OWN walk. except.paths globs are counted
	// against walkIncludingBuiltinSkip (perglob.go), so a live count alone does
	// not prove the engine ever reads the file; this is what distinguishes the
	// two (exceptInertVerdicts).
	byPath := make(map[string]*scan.File, len(fset.Files))
	for _, f := range fset.Files {
		byPath[f.Path()] = f
	}
	rows := make([]row, 0, len(cfg.Rules))
	for _, r := range cfg.Rules {
		rw := row{id: r.ID, typ: r.Type, scopeN: len(scopes[r.ID])}

		// class 1 — an empty scope. formwork lint checks this too, but exempts
		// heavy external-tool rules; the census does not, because a command
		// rule whose scope has rotted still never runs in a --staged lane.
		if rw.scopeN == 0 {
			rw.verdicts = append(rw.verdicts, verdict{
				class: class1, code: "EMPTY-SCOPE", gating: true,
				why: "scope.include matches no file in the tree — the rule can never be reached",
			})
		}

		rw.verdicts = append(rw.verdicts, perGlobVerdicts(r, gm)...)

		// class 2 — the rule whose only in-scope subject is its own detector
		// (#15103). Runs on every rule on every census, unconditionally: the
		// diff-scoped mutation-proof runner is the only other instrument that
		// asks this question, and an untouched rule never reaches it.
		rw.verdicts = append(rw.verdicts, selfProofVerdicts(r, root, scopes[r.ID])...)

		iv, err := exceptInertVerdicts(r, root, gm, byPath)
		if err != nil {
			return nil, err
		}
		rw.verdicts = append(rw.verdicts, iv...)

		m := meta[r.ID]
		rw.existenceObligation = isExistenceObligation(r, m)
		rw.relationObligation = isRelationObligation(r)
		if rw.existenceObligation && rw.scopeN > 0 {
			ws, err := witnesses(r, root, scopes[r.ID])
			if err != nil {
				return nil, err
			}
			for _, w := range ws {
				rw.witnesses = append(rw.witnesses, w.Path())
			}
			if len(ws) > 0 {
				rw.verdicts = append(rw.verdicts, class2Verdicts(r, root, ws, scopes[r.ID])...)
			}
		}
		if rw.relationObligation && rw.scopeN > 0 {
			rw.verdicts = append(rw.verdicts, relationVerdicts(r, root, m, scopes[r.ID])...)
		}
		// Name-anchored go rule types are in NEITHER population above: a
		// confinement is not an existence obligation (deleting every file
		// satisfies it) and not a relation. They get the probe that is valid for
		// them instead — symbol.go, #12494.
		if rw.scopeN > 0 {
			rw.verdicts = append(rw.verdicts, symbolAnchorVerdicts(r, root, scopes[r.ID], declared)...)
		}

		fv, err := fixtureVerdicts(r, root, &rw, gm)
		if err != nil {
			return nil, err
		}
		rw.verdicts = append(rw.verdicts, fv...)
		rw.verdicts = append(rw.verdicts, emptyScopeFireVerdict(&rw)...)
		rw.verdicts = append(rw.verdicts, noFireWitnessVerdict(r, &rw, synths)...)

		rows = append(rows, rw)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].id < rows[j].id })
	return rows, nil
}

// class2Verdicts runs the three evidence probes over a rule's witnesses.
func class2Verdicts(r *config.Rule, root string, ws, inScope []*scan.File) []verdict {
	var out []verdict

	// COMMENT-SUFFICIENT. The question is not whether the comments are NEEDED —
	// that only catches a rule with no real backing left — but whether they are
	// ENOUGH. If the rule still passes on a witness reduced to its comment plane,
	// then every line of the thing it guards could be deleted and the prose about
	// it would hold the gate green. b06-govulncheck is the archetype: real
	// wiring AND comments both name `govulncheck`, so a necessity test reports it
	// live while deleting the whole scan leaves it passing.
	//
	// Exempt when the rule DECLARES that it reads comments: a preprocess:
	// comments-only-* rule is a marker-vocabulary lockdown, for which a comment is
	// the intended carrier. The declaration is the exemption — no list to drift.
	if !strings.HasPrefix(r.Preprocess, "comments-only") {
		for _, f := range ws {
			plane, hasComments := commentPlane(f)
			if !hasComments || !satisfies(r, root, plane) {
				continue
			}
			backing := "NO code-plane backing — the subject is already absent, tightening this rule turns it red"
			if satisfies(r, root, codePlane(f)) {
				backing = "code-plane backing present — tightening this rule keeps it green"
			}
			out = append(out, verdict{
				class: class2, code: "COMMENT-SUFFICIENT", gating: true,
				why: "the witness satisfies this rule on its comment plane alone — every line of the " +
					"subject could be deleted and the prose about it would keep the gate green",
				evidence: append([]string{f.Path() + ": " + backing + cureNote(f.Path())}, commentLines(r, root, f)...),
			})
			break
		}
	}

	// DETECTOR-SATISFIED (#12178 defect 2 — documented in main.go's header since
	// the census landed, never implemented). Every witness is a gate source: the
	// rule asserts that its own detector still contains some text, not that the
	// detector's invariant holds. The subject it names could be deleted whole and
	// the detector's own mention of it would keep the gate green — the same shape
	// as COMMENT-SUFFICIENT one file over.
	if dw := detectorWitnesses(r, ws, inScope); len(dw) > 0 {
		out = append(out, verdict{
			class: class2, code: "DETECTOR-SATISFIED", gating: true,
			why: "every witness is a gate/detector source, not product code — the rule asserts that its own " +
				"detector still contains the token, not that the invariant the detector describes holds",
			evidence: dw,
		})
	}

	// DIFFUSE-EVIDENCE. #12178 defect 3: this arm used to be restricted to
	// len(ws) <= 3, which excluded exactly the rules whose evidence is most
	// redundant — a rule with 44, 53 or 517 witnesses could never appear in the
	// reported 4. The restriction was never about witness COUNT: the verdict
	// requires EVERY witness to be internally redundant, so a rule with many
	// witnesses that each pin one line still passes on the first non-diffuse
	// one, which is also what keeps the loop cheap (it breaks there).
	//
	// Having many witnesses is a different property, and it stays out: that is
	// tools/formwork-witness-census's grain (#12182), and the two must not both
	// claim it.
	{
		diffuse := true
		var ev []string
		for _, f := range ws {
			d := loadBearing(r, root, f, diffuseThreshold)
			if len(d) < diffuseThreshold {
				diffuse = false
				break
			}
			ev = append(ev, fmt.Sprintf("%s: %d+ independently sufficient lines (first: %d)", f.Path(), len(d), d[0]))
		}
		if diffuse {
			out = append(out, verdict{
				class: class2, code: "DIFFUSE-EVIDENCE", gating: true,
				why: fmt.Sprintf("at least %d separate lines of the witness each satisfy the rule on their own, so "+
					"no single regression can trip it — the pattern pins a token, not an invariant", diffuseThreshold),
				evidence: ev,
			})
		}
	}
	return out
}

// detectorWitnesses returns the witness paths when a rule that REACHES product
// code is satisfied only by gate sources — a detector under scripts/ or tools/,
// or the rule's own declared origin.
//
// Both conditions are required, and the second is what keeps the verdict
// honest. A rule whose subject genuinely IS a gate script — "the dart-analyze
// gate cds to the workspace root", "the forensic-grep phrase matcher folds
// case" — can only ever be witnessed by that script, and calling it vacuous
// would condemn every gate-about-a-gate rule in the corpus (14 of them here).
// The defect is the OTHER shape: a rule whose scope reaches real product code,
// where the only file satisfying it is the detector that talks about the
// product. Then the product could be deleted whole and the detector's own
// mention of it would keep the gate green — COMMENT-SUFFICIENT one file over.
func detectorWitnesses(r *config.Rule, ws, inScope []*scan.File) []string {
	isDetector := func(p string) bool {
		return p == r.Origin ||
			strings.HasPrefix(p, "scripts/") ||
			strings.HasPrefix(p, "tools/")
	}
	subject := false
	for _, f := range inScope {
		if !isDetector(f.Path()) {
			subject = true
			break
		}
	}
	if !subject {
		return nil // the rule's whole subject is the gate tree; a gate witness is correct
	}
	var out []string
	for _, f := range ws {
		if !isDetector(f.Path()) {
			return nil
		}
		out = append(out, f.Path())
	}
	return out
}

// cureNote names the cure a COMMENT-SUFFICIENT verdict is answered with.
//
// #12178 defect 4, stated precisely. formwork really does carry no decomment-*
// projection for YAML — the registry is code-only-dart, comments-only-{awk,
// dart,go,sql}, decomment-{go,sh}, decomment-destring-go, destring-{sh,
// decomment-sh}, strings-only-{go,sh} and raw, and nothing else — so the note
// this replaces was right about the projection. It was wrong about the
// CONSEQUENCE: it concluded "real findings with no cure available yet" and
// therefore did not gate. A cure that needs no projection is already in use by
// 22 rules in this corpus — the `^[^#]*` comment-immunity anchor, which makes a
// pattern unmatchable inside a comment without any preprocess at all.
func cureNote(path string) string {
	if curableLang(path) {
		return "; cure: declare the preprocess this rule genuinely reads, or anchor the pattern with `^[^#]*`"
	}
	return "; formwork has no decomment-* projection for this language, so the cure is the `^[^#]*` " +
		"comment-immunity anchor (already used by 22 rules here), not a preprocess declaration"
}

// fixtureVerdicts evaluates the rule's fire/pass fixture trees with the real
// engine and reports a pair that has stopped discriminating.
func fixtureVerdicts(r *config.Rule, root string, rw *row, gm *globMeasure) ([]verdict, error) {
	dir := filepath.Join(root, ".formwork", "fixtures", r.ID)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		// A heavy rule (type:command, git-diff) carries no fixture by convention;
		// whether the lockdown synth it defers to actually exists is
		// noFireWitnessVerdict's question, not this one's. Any other rule
		// without a fixture is a class-3 offence outright.
		if rules.CostOf(r.Checker) == rules.CostHeavy {
			return nil, nil
		}
		return []verdict{{
			class: class3, code: "NO-FIXTURE", gating: true,
			why: "no fixture tree — nothing ever demonstrates that this rule can fail",
		}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", dir, err)
	}

	var out []verdict
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		isFire := strings.HasPrefix(e.Name(), "fire-")
		if !isFire && !strings.HasPrefix(e.Name(), "pass-") {
			return nil, fmt.Errorf("%s: unrecognized fixture dir %q, expected fire-* or pass-*", dir, e.Name())
		}
		rw.hasFixtures = true
		ffs, err := scan.Walk(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		fresh, err := r.Fresh()
		if err != nil {
			return nil, err
		}
		fresh.Allowlist = nil // allowlist paths are repo-relative, fixture paths are not
		fds, err := engine.Run([]*config.Rule{fresh}, ffs, 1)
		if err != nil {
			return nil, fmt.Errorf("rule %s fixture %s: %w", r.ID, e.Name(), err)
		}
		live := finding.Unsuppressed(fds)
		if isFire {
			rw.fireCount++
			for _, ff := range ffs.Files {
				if r.Applies(ff.Path()) {
					rw.fireInScope++
				}
			}
			if len(live) == 0 {
				out = append(out, verdict{
					class: class3, code: "FIRE-FIXTURE-PASSES", gating: true,
					why:      "the violation fixture no longer trips the rule — the pair has stopped discriminating",
					evidence: []string{".formwork/fixtures/" + r.ID + "/" + e.Name()},
				})
			}
			continue
		}
		rw.passCount++
		if len(live) > 0 {
			out = append(out, verdict{
				class: class3, code: "PASS-FIXTURE-FIRES", gating: true,
				why:      "the clean fixture trips the rule — the pair has stopped discriminating",
				evidence: []string{".formwork/fixtures/" + r.ID + "/" + e.Name()},
			})
			continue
		}
		pv, err := passVacuousVerdict(r, e.Name(), ffs, gm)
		if err != nil {
			return nil, err
		}
		out = append(out, pv...)
	}
	if rw.hasFixtures && (rw.fireCount == 0 || rw.passCount == 0) {
		out = append(out, verdict{
			class: class3, code: "HALF-FIXTURE", gating: true,
			why: fmt.Sprintf("fixture pair is one-sided (%d fire, %d pass) — one direction is never asserted",
				rw.fireCount, rw.passCount),
		})
	}
	return out, nil
}

// emptyScopeFireVerdict is what the census's "absence-only fixtures" number was
// reaching for, expressed so it discriminates (#12178).
//
// The measurement it replaces reported every rule whose fire findings all lack
// a Path, on the reading that such a rule is only ever shown firing on DELETED
// evidence, never on WRONG evidence. The reading is right; the detector is not.
// A finding gets its Path from engine.toFinding's fallback — the file being
// checked — and required-pattern mode:exists, pattern-count and set-relation
// all emit their verdict from Finalize, where there is no file to fall back to.
// A Path is therefore not something ANY fixture of those three types can
// produce, and measured against this corpus those three types were the entire
// population: 324 + 82 + 89 of the 512, with the balance type:command. The
// number was a restatement of the rule type, carrying no information about a
// single fixture. Gating it would have demanded of 512 authors a thing the
// engine cannot emit.
//
// What DOES discriminate, at the same grain: a fire fixture with no in-scope
// file at all. pattern-count op:at-least fires on an empty tree (total 0 < n),
// so such a fixture demonstrates the SCOPE is empty and nothing whatever about
// the pattern — the fixture-level twin of class 1's empty scope. The cure is a
// fire fixture that carries the in-scope file with the evidence wrong.
func emptyScopeFireVerdict(rw *row) []verdict {
	if !rw.hasFixtures || rw.fireCount == 0 || rw.fireInScope > 0 {
		return nil
	}
	return []verdict{{
		class: class3, code: "EMPTY-SCOPE-FIRE-FIXTURE", gating: true,
		why: fmt.Sprintf("not one of the %d fire fixture(s) contains a file this rule's scope matches, so each "+
			"of them fires because the scope is EMPTY, not because the evidence is wrong — the fixture-level "+
			"twin of class 1. Put the in-scope file in the fixture and make its evidence wrong", rw.fireCount),
	}}
}

// noFireWitnessVerdict answers the heavy-rule fixture exemption (#12178).
//
// fixtureVerdicts lets a type:command or git-diff rule carry no fixture at all,
// on the grounds that "their fire coverage lives in
// api-factory/internal/lockdowntests/". That was asserted and never checked,
// and it demonstrably failed: synth_form_coverage_markers_test.go was twelve
// lines of `// FORM(...)` markers under a lockdown build tag with no test
// function in it (deleted in #12238, which locked the shape out with
// synth-form-marker-colocation). A rule witnessed only by a file like that has
// nothing, anywhere, demonstrating it can fail — which is the whole subject of
// this census. So the exemption is now CONDITIONAL on the coverage it claims:
// some file under lockdowntests must name the rule AND declare a test function.
func noFireWitnessVerdict(r *config.Rule, rw *row, synths map[string]bool) []verdict {
	if rw.hasFixtures || rules.CostOf(r.Checker) != rules.CostHeavy || synths[r.ID] {
		return nil
	}
	return []verdict{{
		class: class3, code: "NO-FIRE-WITNESS", gating: true,
		why: "a heavy rule carries no fixture, and no test function under " + lockdownSynthDirRel +
			" names it — nothing anywhere has ever demonstrated this rule failing. Add the lockdown synth " +
			"that fires it, or give the rule a fire/pass fixture pair",
	}}
}

func paths(fs []*scan.File) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.Path())
	}
	return out
}
