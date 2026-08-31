// narrowable.go — the third way a rule ARM can be written so that it cannot
// fail: a `required-pattern` whose pattern is a PREFIX of its own narrowing.
//
// #11934. `defaulttemplates-formula-validate-walks-sections-items` pinned the
// literal `range spec\.Sections` over formula_validate.go. That literal is a
// prefix of every legal way to reduce the collection: `range spec.Sections`
// still matches `range spec.Sections[:1]`. Measured on develop — narrowing all
// four loops in that file to their first member compiled, left the package's
// own tests green, and left the arm and its two siblings reporting OK, while
// formula validation for every section, item, option and kit after the first
// silently stopped running.
//
// The verdict is DEMONSTRATED, not inferred from the pattern's spelling. For
// each arm the census reads the lines its own scope and preprocessing actually
// produce, finds the ones where the pattern's match ENDS inside a traversal's
// source expression — `range <expr>` in Go, `for (… in <expr>)` in Dart — and
// re-runs the pattern against that same line with the expression narrowed. An
// arm is flagged only when a concrete, legal, silent narrowing of a line in its
// own scope leaves it green, and the report prints that line.
//
// Why the match END and not merely "the pattern mentions range". A pattern like
// `WithTenantOrg\(` can match a line that happens to contain a loop head
// without pinning the loop at all; narrowing that loop cannot be laid at the
// arm's door. A pattern whose match STOPS inside the traversed expression is
// the one that has staked its verdict on that expression and left the
// continuation unbounded — the arm still matches whatever is appended to it.
// A pattern that reaches its own terminator (`\)`, `\s*\{`, `\s`) is spared by
// construction, because the narrowing displaces that terminator.
package main

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

type lang int

// The two source languages where a traversal source can be narrowed by a
// SUFFIX and stay legal: Go's `xs[:1]` and Dart's `xs.take(1)`. Everything
// else is langOther and is never asked the question.
const (
	langOther lang = iota
	langGo
	langDart
)

// span is a half-open byte range within one line.
type span struct{ start, end int }

// langOf classifies a scanned file by extension.
func langOf(path string) lang {
	switch {
	case strings.HasSuffix(path, ".go"):
		return langGo
	case strings.HasSuffix(path, ".dart"):
		return langDart
	}
	return langOther
}

// narrowing is the shortest legal suffix that reduces a traversal source to
// its first member in l, leaving the surrounding statement compiling.
func (l lang) narrowing() string {
	switch l {
	case langGo:
		return "[:1]"
	case langDart:
		return ".take(1)"
	}
	return ""
}

// goRangeHead matches Go's `range ` keyword. The excluded leading characters
// are what keeps `--diff-range` and `foo.range` out: `range` is a keyword only
// where an identifier could not continue into it, and it opens an expression
// only when whitespace follows.
//
// dartForInHead matches the `in` of a `for (… in …)` header. Anchoring on
// `for (` is deliberate — a bare ` in ` is ordinary English and appears in
// every second string literal and doc comment.
//
// dartForInWrapped is the SAME header after dart format has broken it. Past
// the line limit the formatter wraps BEFORE the `in`, leaving the traversal
// source alone on a continuation line, and the repo's dart-format gate makes
// that the only legal spelling of a long loop. Anchoring on a line that BEGINS
// with `in` is what keeps prose out: Dart has no statement that starts with
// the `in` keyword, so a leading `in` at the head of a line is a wrapped
// for-in and nothing else. Reading only the single-line form would catch the
// shape a fixture author writes and miss the shape the formatter produces
// (#15721).
var (
	goRangeHead      = regexp.MustCompile(`(^|[^\p{L}\p{Nd}_.\-])range[ \t]+`)
	dartForInHead    = regexp.MustCompile(`\bfor\s*\(\s*[^;()]*?\s+in\s+`)
	dartForInWrapped = regexp.MustCompile(`^\s*in[ \t]+`)
)

// exprEnd walks the expression starting at i and returns the offset one past
// its last byte. Brackets nest, so an argument list is consumed whole and a
// narrowing is never spliced into the middle of one; an unbalanced closer, a
// separator or a block brace ends it.
func exprEnd(line string, i int) int {
	depth := 0
	for i < len(line) {
		switch c := line[i]; {
		case c == '(' || c == '[':
			depth++
		case c == ')' || c == ']':
			if depth == 0 {
				return i
			}
			depth--
		case depth == 0 && (c == ' ' || c == '\t' || c == '{' || c == ',' || c == ';' || c == '}'):
			return i
		}
		i++
	}
	return i
}

// traversalSpans locates every traversal SOURCE expression in line: the text
// after `range` in Go, and after the `in` of a `for (… in …)` in Dart. The
// expression is bounded by its own brackets, so `range f(a, b) {` yields
// `f(a, b)` and not `f(a`.
func traversalSpans(line string, l lang) []span {
	var heads []*regexp.Regexp
	switch l {
	case langGo:
		heads = []*regexp.Regexp{goRangeHead}
	case langDart:
		heads = []*regexp.Regexp{dartForInHead, dartForInWrapped}
	default:
		return nil
	}
	var out []span
	for _, head := range heads {
		for _, m := range head.FindAllStringIndex(line, -1) {
			if e := exprEnd(line, m[1]); e > m[1] {
				out = append(out, span{start: m[1], end: e})
			}
		}
	}
	return out
}

// narrowableOn reports whether m's verdict on line survives narrowing one of
// that line's traversals, and returns the narrowed line that defeats it.
func narrowableOn(m lineMatcher, line string, l lang) (string, bool, error) {
	spans := traversalSpans(line, l)
	if len(spans) == 0 {
		return "", false, nil
	}
	loc, err := m.find(line)
	if err != nil || loc == nil {
		return "", false, err
	}
	for _, sp := range spans {
		// The arm has staked its verdict on this traversal only if its match
		// STOPS inside the ranged expression. A match that runs past the
		// expression is displaced by the narrowing; a match that ends before
		// it never pinned the loop at all.
		if loc[1] < sp.start || loc[1] > sp.end {
			continue
		}
		narrowed := line[:sp.end] + l.narrowing() + line[sp.end:]
		switch ok, err := m.matches(narrowed); {
		case err != nil:
			return "", false, err
		case ok:
			return narrowed, true, nil
		}
	}
	return "", false, nil
}

// detectNarrowable flags every required-pattern arm with a narrowable witness
// in its own scope.
func detectNarrowable(root string, arms []arm) ([]offender, int, error) {
	cfg, err := config.Load(root)
	if err != nil {
		return nil, 0, err
	}
	byID := make(map[string]*config.Rule, len(cfg.Rules))
	for _, r := range cfg.Rules {
		byID[r.ID] = r
	}
	fset, err := scan.Walk(root)
	if err != nil {
		return nil, 0, err
	}
	var bad []offender
	examined := 0
	for _, a := range arms {
		if a.Type != "required-pattern" || a.Pattern == "" {
			continue
		}
		r, ok := byID[a.ID]
		if !ok {
			// The corpus reader saw an arm the engine did not load. A
			// loader disagreement is an error, never a quiet skip.
			return nil, 0, fmt.Errorf("%s:%d: arm %q is declared but the engine did not load it", a.File, a.Line, a.ID)
		}
		examined++
		m, err := compileLine(a.Pattern, a.Syntax)
		if err != nil {
			return nil, 0, fmt.Errorf("%s:%d (%s): %w", a.File, a.Line, a.ID, err)
		}
		o, err := narrowableWitness(a, r, m, fset.Files)
		if err != nil {
			return nil, 0, err
		}
		if o != nil {
			bad = append(bad, *o)
		}
	}
	return bad, examined, nil
}

// narrowableWitness returns the first line in r's scope that defeats a, read
// through r's own preprocessing so the census sees exactly the text the gate
// sees. One witness is enough: the cure is to bound the pattern, which removes
// every witness at once.
func narrowableWitness(a arm, r *config.Rule, m lineMatcher, files []*scan.File) (*offender, error) {
	for _, f := range files {
		if !r.Applies(f.Path()) {
			continue
		}
		l := langOf(f.Path())
		if l == langOther {
			continue
		}
		v, err := f.Variant(r.Preprocess)
		if err != nil {
			return nil, fmt.Errorf("%s: %s: %w", a.ID, f.Path(), err)
		}
		lines, err := v.Lines()
		if err != nil {
			return nil, fmt.Errorf("%s: %s: %w", a.ID, f.Path(), err)
		}
		for i, ln := range lines {
			narrowed, yes, err := narrowableOn(m, ln, l)
			if err != nil {
				return nil, fmt.Errorf("%s: %s: %w", a.ID, f.Path(), err)
			}
			if !yes {
				continue
			}
			return &offender{a.File, a.Line, a.ID, fmt.Sprintf(
				"pattern %q stops inside a traversal source, so a narrowing appended past its match keeps it green. "+
					"%s:%d reads\n       %s\n     and the arm still matches\n       %s\n"+
					"     Bound the pattern past the ranged expression (`\\s*\\{`, `\\)`, `\\s`), or replace it with a "+
					"runtime assertion that counts what the walk visited.",
				a.Pattern, f.Path(), i+1, strings.TrimSpace(ln), strings.TrimSpace(narrowed))}, nil
		}
	}
	return nil, nil
}
