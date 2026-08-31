package main

// perglob.go — the per-glob grain (#10626).
//
// config.Rule.Applies is a UNION predicate: it answers "does this rule watch
// this file" and destroys, by construction, which include glob earned the
// match. That is the right abstraction for evaluating a rule and the wrong
// one for auditing it: a rule watching six places, five of which moved away,
// still applies to something, so both whole-rule instruments (formwork lint's
// empty-scope, this census's class 1) report it healthy. This file answers
// the finer question — does every place the rule claims to watch still exist —
// by matching each declared scope glob INDIVIDUALLY against the same
// scan.Walk FileSet the whole-rule scope uses.
//
// Matching still goes through formwork's own matcher: each glob is compiled
// into a one-glob config.Rule and counted with Applies, the same code the
// gate engine runs. Never the shell — `ls`/`find` cannot expand `**`, which
// is the error that produced #10083's 133 false zeros.
//
// YAML aliases are resolved (#10876): `include: *bootstraps` and
// `scope: *shared` are AliasNodes when walked as yaml.Node, and without
// resolving them the rule contributed zero globs and opted out of per-glob
// scrutiny. The engine's typed unmarshal expands aliases, so the rule still
// ran — only this census was blind. resolveNode follows Alias → anchor.
//
// # The in-place escape hatch
//
// A genuinely aspirational glob (a tree that will exist) is declared dead IN
// PLACE, with a reason, never via a separate allowlist file — the same shape
// as find-unresolvable-gate-targets.go's `# gate-target:` annotation:
//
//	include:
//	  - "src/live/**/*.go"
//	  # glob-dead: lands with the pending src/moved migration
//	  - "src/moved/**/*.go"
//
// The comment must sit on the line directly above the glob and carry a
// non-empty reason; a bare `# glob-dead:` does not exempt.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/finding"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"gopkg.in/yaml.v3"
)

// globCount is one declared glob and the number of files it alone matches.
type globCount struct {
	glob   string
	n      int
	deadOK bool // declared via `# glob-dead: <reason>` on the line above
	declOK bool // declared via `# except-declaration: <reason>` (except.paths only)
	reason string
}

// globMeasure is the per-glob scope census: for every rule, the match count
// of each scope.include / scope.exclude / except.paths glob taken on its own.
type globMeasure struct {
	include map[string][]globCount
	exclude map[string][]globCount
	except  map[string][]globCount

	totalInclude int // scope.include globs examined, corpus-wide

	// sourceLists are the scope.include lists a rule DECLARES to be exhaustive
	// over a source directory, via `# source-list-exhaustive: <dir>` (#13517).
	// Read by sourcelist.go; empty for every rule that has not opted in.
	sourceLists []sourceListDecl
}

// scannedPaths is the engine's own scan.Walk FileSet as repo-relative slash
// paths. It is the denominator every INCLUDE glob is counted against, and the
// one the source-list arm derives a package's real sources from — so no coverage
// claim in this module is ever measured over files the engine will not read.
// (Subtractive globs use walkIncludingBuiltinSkip instead; see measureGlobs.)
func scannedPaths(fset *scan.FileSet) []string {
	out := make([]string, 0, len(fset.Files))
	for _, f := range fset.Files {
		out = append(out, f.Path())
	}
	return out
}

// countGlobMatches counts the files ONE glob matches, using formwork's own
// matcher (a one-glob rule's Applies) rather than a re-implementation.
func countGlobMatches(glob string, paths []string) (int, error) {
	r, err := config.New("perglob", "forbidden-pattern", finding.SeverityError, "",
		[]string{glob}, nil, nil, nil)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, p := range paths {
		if r.Applies(p) {
			n++
		}
	}
	return n, nil
}

// measureGlobs re-reads .formwork/rules/*.yaml — config.Rule keeps include and
// exclude private, and the `# glob-dead:` comment plane exists only in the
// source text — then counts every declared glob against the walked tree.
// Matching is cached per glob text; the corpus reuses globs heavily.
func measureGlobs(root string, fset *scan.FileSet) (*globMeasure, error) {
	paths := scannedPaths(fset)

	// An EXCLUDE or an EXCEPT is measured against the tree WITHOUT the engine's
	// built-in skip, not against the scanned FileSet (#12178).
	//
	// scan.Walk drops .git and .formwork before any rule runs, so an exclude
	// naming `.formwork/**` matches zero SCANNED files while naming thousands of
	// real ones. formwork-engine-skip-declared REQUIRES exactly that declaration
	// on every content rule whose include reaches a .formwork path — "add
	// '.formwork/**' to scope.exclude … so the written scope equals the scanned
	// scope" — so measuring it post-skip would make the two rules demand
	// opposite things, and gating on the post-skip count would fail 163 globs
	// another rule mandates. scan.UnderBuiltinSkip exists for precisely this
	// reasoning: callers reasoning about paths the walk never enumerated must
	// not attribute a built-in skip to an operator glob.
	//
	// The distinction is only ever generous to the author — an exclude glob's
	// job is to SUBTRACT, so counting it over a superset can never turn a live
	// exclude into a dead one.
	rawPaths, err := walkIncludingBuiltinSkip(root)
	if err != nil {
		return nil, err
	}

	gm := &globMeasure{
		include: map[string][]globCount{},
		exclude: map[string][]globCount{},
		except:  map[string][]globCount{},
	}
	cache := map[string]int{}
	count := func(glob string) (int, error) {
		if n, ok := cache[glob]; ok {
			return n, nil
		}
		n, err := countGlobMatches(glob, paths)
		if err != nil {
			return 0, err
		}
		cache[glob] = n
		return n, nil
	}
	rawCache := map[string]int{}
	countRaw := func(glob string) (int, error) {
		if n, ok := rawCache[glob]; ok {
			return n, nil
		}
		n, err := countGlobMatches(glob, rawPaths)
		if err != nil {
			return 0, err
		}
		rawCache[glob] = n
		return n, nil
	}

	files, err := filepath.Glob(filepath.Join(root, ".formwork", "rules", "*.yaml"))
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		refs, decls, err := parseRuleGlobs(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		gm.sourceLists = append(gm.sourceLists, decls...)
		for _, ref := range refs {
			measure := count
			if ref.key != "include" {
				measure = countRaw
			}
			n, err := measure(ref.glob)
			if err != nil {
				return nil, fmt.Errorf("%s: rule %s: %w", f, ref.ruleID, err)
			}
			gc := globCount{glob: ref.glob, n: n, deadOK: ref.deadOK, declOK: ref.declOK, reason: ref.reason}
			switch ref.key {
			case "include":
				gm.include[ref.ruleID] = append(gm.include[ref.ruleID], gc)
				gm.totalInclude++
			case "exclude":
				gm.exclude[ref.ruleID] = append(gm.exclude[ref.ruleID], gc)
			case "except":
				gm.except[ref.ruleID] = append(gm.except[ref.ruleID], gc)
			}
		}
	}
	return gm, nil
}

// walkIncludingBuiltinSkip enumerates every regular file under root as a
// repo-relative slash path, WITHOUT the engine's built-in .git/.formwork prune.
// It is the denominator for exclude/except globs only (see measureGlobs); every
// include glob is still counted against the engine's own scan.Walk FileSet, so
// no coverage claim is ever measured over files the engine will not read.
func walkIncludingBuiltinSkip(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// globRef is one glob as declared in a rule file, before matching.
type globRef struct {
	ruleID string
	key    string // "include" | "exclude" | "except"
	glob   string
	deadOK bool
	declOK bool
	reason string
}

// sourceListDecl is one rule's claim that its scope.include enumerates a source
// directory EXHAUSTIVELY (#13517):
//
//	scope:
//	  # source-list-exhaustive: tools/formwork-vacuity-census
//	  include:
//	    - "tools/formwork-vacuity-census/main.go"
//	    - "tools/formwork-vacuity-census/classify.go"
//
// The marker sits on the line directly above the `include:` KEY — it is a claim
// about the whole list, not about any one glob, which is what distinguishes it
// from `# glob-dead:` and `# except-declaration:` (both per-entry). Same
// non-empty-reason convention as those two: a bare `# source-list-exhaustive:`
// declares nothing and does not subscribe.
//
// dir is the value as written, repo-relative and slash-separated. The globs are
// the rule's include list verbatim, so the arm can ask which sources the list
// names WITHOUT re-deriving them from a match count — a file present in the
// directory and absent from the list matches no glob and would otherwise be
// invisible, which is the whole defect.
type sourceListDecl struct {
	ruleID string
	dir    string
	globs  []string
}

// parseRuleGlobs walks the yaml.Node tree (not a typed struct) so every glob
// scalar carries its line number; the line above it is where a `# glob-dead:`
// declaration must sit. Line numbers also make the declaration work uniformly
// for block (`- "glob"`) and inline (`include: ["a", "b"]`) forms.
//
// AliasNodes are resolved before inspection (#10876): without that,
// `include: *bootstraps` and `scope: *shared` contribute zero globs and the
// rule silently opts out of per-glob scrutiny while the engine (typed
// unmarshal) still expands them.
// It also returns the `# source-list-exhaustive:` declarations (#13517), read
// from the same line-numbered walk: the marker is a comment, so it exists only
// in the source text, exactly like the two per-entry markers above.
func parseRuleGlobs(data []byte) ([]globRef, []sourceListDecl, error) {
	lines := strings.Split(string(data), "\n")
	declaredWith := func(marker string, line int) (bool, string) {
		if line < 2 {
			return false, ""
		}
		above := strings.TrimSpace(lines[line-2])
		rest, ok := strings.CutPrefix(above, marker)
		if !ok {
			return false, ""
		}
		reason := strings.TrimSpace(rest)
		return reason != "", reason
	}
	declaredDead := func(line int) (bool, string) { return declaredWith("# glob-dead:", line) }
	// `# except-declaration: <reason>` (#10777) is recognised on except.paths
	// ONLY, and is deliberately separate vocabulary from `# glob-dead:`. They
	// answer different questions: glob-dead says the glob matches nothing by
	// design, except-declaration says the path it matches is the subject's
	// DECLARATION home — a file that can never be a violator, such as the field
	// declaration a forbidden-pattern rule is written against. Collapsing them
	// would let "matches nothing" excuse "matches a file the rule cannot fire
	// on", which is the claim INERT-EXCEPT exists to separate.
	//
	// Scoping recognition to except.paths keeps a misplaced marker from reading
	// as coverage: on an include glob it is simply not recognised, and if that
	// glob is dead EMPTY-GLOB still fires and says so.
	declaredException := func(line int) (bool, string) { return declaredWith("# except-declaration:", line) }

	// `# source-list-exhaustive: <dir>` (#13517) is recognised on the scope.include
	// KEY only. The two markers above are per-entry claims; this one is about the
	// LIST — that every non-test source in <dir> appears in it — so anchoring it to
	// an entry would make it read as a statement about that entry alone.
	declaredSourceList := func(line int) (bool, string) {
		return declaredWith("# source-list-exhaustive:", line)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, nil, err
	}
	var refs []globRef
	var decls []sourceListDecl
	for _, rule := range mappingSeq(&doc, "rules") {
		id := mappingScalar(rule, "id")
		for _, key := range []string{"include", "exclude"} {
			var globs []string
			for _, g := range mappingSeq(mappingValue(rule, "scope"), key) {
				g = resolveNode(g)
				if g == nil || g.Kind != yaml.ScalarNode {
					continue
				}
				deadOK, reason := declaredDead(g.Line)
				refs = append(refs, globRef{id, key, g.Value, deadOK, false, reason})
				globs = append(globs, g.Value)
			}
			if key != "include" {
				continue
			}
			if ok, dir := declaredSourceList(mappingKeyLine(mappingValue(rule, "scope"), key)); ok {
				decls = append(decls, sourceListDecl{ruleID: id, dir: dir, globs: globs})
			}
		}
		for _, g := range mappingSeq(mappingValue(rule, "except"), "paths") {
			g = resolveNode(g)
			if g == nil || g.Kind != yaml.ScalarNode {
				continue
			}
			deadOK, reason := declaredDead(g.Line)
			declOK, declReason := declaredException(g.Line)
			if declOK {
				reason = declReason
			}
			refs = append(refs, globRef{id, "except", g.Value, deadOK, declOK, reason})
		}
	}
	return refs, decls, nil
}

// resolveNode follows YAML alias nodes to their anchors. Typed unmarshal does
// this automatically; walking yaml.Node does not (#10876).
func resolveNode(n *yaml.Node) *yaml.Node {
	for n != nil && n.Kind == yaml.AliasNode {
		n = n.Alias
	}
	return n
}

// mappingValue returns the value node for key in a mapping node, or nil.
// The returned node is alias-resolved so callers see the anchored content.
func mappingValue(n *yaml.Node, key string) *yaml.Node {
	n = resolveNode(n)
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return resolveNode(n.Content[i+1])
		}
	}
	return nil
}

// mappingKeyLine returns the 1-based line of the KEY node for key in a mapping,
// or 0 when the key is absent. mappingValue deliberately returns the VALUE, and
// a sequence value's line is its first item — so a declaration written above
// `include:` would be read against the wrong line. A whole-list marker
// (`# source-list-exhaustive:`, #13517) is anchored to the key for the same
// reason a per-glob one is anchored to its scalar: the line above the thing the
// claim is about.
func mappingKeyLine(n *yaml.Node, key string) int {
	n = resolveNode(n)
	if n == nil || n.Kind != yaml.MappingNode {
		return 0
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i].Line
		}
	}
	return 0
}

// mappingScalar returns the scalar value for key in a mapping node.
func mappingScalar(n *yaml.Node, key string) string {
	if v := mappingValue(n, key); v != nil {
		return v.Value
	}
	return ""
}

// mappingSeq returns the item nodes of the sequence stored under key in a
// mapping node — or, when n IS the document node, of the sequence under key in
// the document's root mapping. A nil or non-sequence yields nil. AliasNodes on
// the sequence itself (include: *alias) and on the parent mapping
// (scope: *shared) are resolved first (#10876).
func mappingSeq(n *yaml.Node, key string) []*yaml.Node {
	if n == nil {
		return nil
	}
	n = resolveNode(n)
	if n.Kind == yaml.DocumentNode {
		if len(n.Content) == 0 {
			return nil
		}
		n = resolveNode(n.Content[0])
	}
	seq := mappingValue(n, key) // already resolved
	if seq == nil || seq.Kind != yaml.SequenceNode {
		return nil
	}
	return seq.Content
}
