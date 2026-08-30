package dartscan

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"gopkg.in/yaml.v3"
)

type gateReadsParamsYAML struct {
	Builders      []string `yaml:"builders"`
	ListenArgs    []string `yaml:"listen_args"`
	BuilderArg    string   `yaml:"builder_arg"`
	ReadSuffixes  []string `yaml:"read_suffixes"`
	IgnoreReaders []string `yaml:"ignore_readers"`
}

// gateReadsAreListened flags a rebuild-scoped builder whose body decides
// something from a listenable it was not given.
//
// AnimatedBuilder/ListenableBuilder rebuild when one of the listenables they
// are handed notifies. Their builder computes a decision — typically an
// enable/disable gate — from a set of controllers. A controller the decision
// READS but the widget does not LISTEN to cannot move the decision: the builder
// re-runs only for the listenables in the merge list, so the gate is served
// from a stale build and does not gate.
//
// STRUCTURAL, not lexical. The declaration and the read are in different
// places by construction: the merge list is an argument of the builder
// invocation, the decision is usually a getter declared elsewhere in the class,
// and the read is one indirection deep inside it. Nothing local to a line or a
// window relates them, which is the #8677 shape.
//
// The scan is FILE-scoped, which is the granularity the language gives: a State
// class and the getters its build method calls live in one file. A getter is
// resolved only when the builder body names it, so an unrelated getter
// elsewhere in the file contributes nothing.
type gateReadsAreListened struct {
	builders     map[string]bool
	listenArgs   []string
	builderArg   string
	readSuffixes []string
	ignore       map[string]bool
}

func newGateReadsAreListened(node *yaml.Node) (rules.Checker, error) {
	var p gateReadsParamsYAML
	if node != nil {
		if err := node.Decode(&p); err != nil {
			return nil, fmt.Errorf("dart/gate-reads-are-listened: %w", err)
		}
	}
	if len(p.Builders) == 0 {
		return nil, errors.New("dart/gate-reads-are-listened: params.builders is required")
	}
	if len(p.ListenArgs) == 0 {
		return nil, errors.New("dart/gate-reads-are-listened: params.listen_args is required")
	}
	if p.BuilderArg == "" {
		return nil, errors.New("dart/gate-reads-are-listened: params.builder_arg is required")
	}
	if len(p.ReadSuffixes) == 0 {
		return nil, errors.New("dart/gate-reads-are-listened: params.read_suffixes is required")
	}
	c := &gateReadsAreListened{
		listenArgs:   p.ListenArgs,
		builderArg:   p.BuilderArg,
		readSuffixes: p.ReadSuffixes,
		builders:     map[string]bool{},
		ignore:       map[string]bool{},
	}
	for _, b := range p.Builders {
		c.builders[b] = true
	}
	for _, r := range p.IgnoreReaders {
		c.ignore[r] = true
	}
	return c, nil
}

func (c *gateReadsAreListened) CheckFile(f *scan.File) ([]rules.Match, error) {
	if !isDart(f) {
		return nil, nil
	}
	content, err := f.Content()
	if err != nil {
		return nil, err
	}
	src := string(content)
	// No builder named here means no rebuild scope to get wrong. Skips the
	// argument-list walk for the bulk of the corpus.
	any := false
	for b := range c.builders {
		if strings.Contains(src, b) {
			any = true
			break
		}
	}
	if !any {
		return nil, nil
	}

	getters := gettersIn(src)
	var matches []rules.Match
	for _, inv := range invocations(src) {
		if !c.builders[inv.name] {
			continue
		}
		args, err := namedArgs(src, inv.open)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %s(: %w", f.Path(), lineAt(src, inv.nameStart), inv.name, err)
		}
		listenExpr, ok := c.firstArg(args, c.listenArgs)
		if !ok {
			continue // declares no listen set; not this rule's subject
		}
		body, ok := args[c.builderArg]
		if !ok {
			continue
		}
		listened := map[string]bool{}
		for _, id := range dartRefs(listenExpr) {
			listened[id] = true
		}
		var missing []string
		seen := map[string]bool{}
		for _, r := range c.readsIn(body, getters, map[string]bool{}) {
			if listened[r] || c.ignore[r] || seen[r] {
				continue
			}
			seen[r] = true
			missing = append(missing, r)
		}
		if len(missing) == 0 {
			continue
		}
		sort.Strings(missing)
		matches = append(matches, rules.Match{
			Line: lineAt(src, inv.nameStart),
			Message: fmt.Sprintf(
				"%s(...) decides from %s, which %s does not listen to — the builder never re-runs when %s changes, so the decision is served from a stale build",
				inv.name, strings.Join(missing, ", "), c.listenArgs[0], strings.Join(missing, ", ")),
		})
	}
	return matches, nil
}

func (c *gateReadsAreListened) firstArg(args map[string]string, names []string) (string, bool) {
	for _, n := range names {
		if v, ok := args[n]; ok {
			return v, true
		}
	}
	return "", false
}

// readsIn returns every listenable the expression decides from: the direct
// `<ref><suffix>` reads, plus the reads of any getter the expression NAMES.
// depth guards a getter that refers to itself.
func (c *gateReadsAreListened) readsIn(expr string, getters map[string]string, visiting map[string]bool) []string {
	var out []string
	for _, suf := range c.readSuffixes {
		out = append(out, refsReadWithSuffix(expr, suf)...)
	}
	for name, body := range getters {
		if visiting[name] || !namesToken(expr, name) {
			continue
		}
		visiting[name] = true
		out = append(out, c.readsIn(body, getters, visiting)...)
		delete(visiting, name)
	}
	return out
}

// refsReadWithSuffix finds every `<dotted-ref><suffix>` in expr and returns the
// dotted ref. Types are dropped: Dart spells classes UpperCamel and instance
// fields lowerCamel or _underscore, so an upper-case leading segment is a
// static access, not a listenable this widget could hold.
func refsReadWithSuffix(expr, suffix string) []string {
	var out []string
	for i := 0; i+len(suffix) <= len(expr); i++ {
		if expr[i:i+len(suffix)] != suffix {
			continue
		}
		// The character after the suffix must not continue an identifier, so
		// `.text` does not match inside `.textStyle`.
		if j := i + len(suffix); j < len(expr) && isIdentByte(expr[j]) {
			continue
		}
		start := i
		for start > 0 && (isIdentByte(expr[start-1]) || expr[start-1] == '.') {
			start--
		}
		ref := expr[start:i]
		if ref == "" || !isLowerRef(ref) {
			continue
		}
		out = append(out, ref)
	}
	return out
}

// dartRefs returns the dotted instance references in an expression, dropping
// type names and call targets by the same upper-case convention.
func dartRefs(expr string) []string {
	var out []string
	i := 0
	for i < len(expr) {
		if !isIdentStart(expr[i]) {
			i++
			continue
		}
		start := i
		for i < len(expr) && (isIdentByte(expr[i]) || expr[i] == '.') {
			i++
		}
		ref := strings.TrimSuffix(expr[start:i], ".")
		if isLowerRef(ref) {
			out = append(out, ref)
		}
	}
	return out
}

// isLowerRef reports whether a dotted reference names an instance rather than a
// type — its first segment starts lower-case or with `_`.
func isLowerRef(ref string) bool {
	if ref == "" {
		return false
	}
	c := ref[0]
	return c == '_' || (c >= 'a' && c <= 'z')
}

// namesToken reports whether expr uses name as a whole identifier.
func namesToken(expr, name string) bool {
	for i := 0; i+len(name) <= len(expr); i++ {
		if expr[i:i+len(name)] != name {
			continue
		}
		if i > 0 && (isIdentByte(expr[i-1]) || expr[i-1] == '.') {
			continue
		}
		if j := i + len(name); j < len(expr) && isIdentByte(expr[j]) {
			continue
		}
		return true
	}
	return false
}

// gettersIn maps each `get <name>` declaration in src to its body, for both the
// `=>` and block forms. A getter is the indirection the live defect hid behind:
// the builder names `_complete` and the controllers it reads are a declaration
// away.
func gettersIn(src string) map[string]string {
	out := map[string]string{}
	for i := 0; i+4 < len(src); i++ {
		if src[i:i+3] != "get" {
			continue
		}
		if i > 0 && isIdentByte(src[i-1]) {
			continue
		}
		j := i + 3
		if j >= len(src) || !(src[j] == ' ' || src[j] == '\t') {
			continue
		}
		for j < len(src) && (src[j] == ' ' || src[j] == '\t') {
			j++
		}
		nameStart := j
		for j < len(src) && isIdentByte(src[j]) {
			j++
		}
		name := src[nameStart:j]
		if name == "" {
			continue
		}
		for j < len(src) && (src[j] == ' ' || src[j] == '\t' || src[j] == '\n') {
			j++
		}
		switch {
		case j+1 < len(src) && src[j] == '=' && src[j+1] == '>':
			end := strings.IndexByte(src[j:], ';')
			if end < 0 {
				continue
			}
			out[name] = src[j+2 : j+end]
		case j < len(src) && src[j] == '{':
			depth := 0
			k := j
			for ; k < len(src); k++ {
				if src[k] == '{' {
					depth++
				} else if src[k] == '}' {
					depth--
					if depth == 0 {
						break
					}
				}
			}
			if k >= len(src) {
				continue
			}
			out[name] = src[j+1 : k]
		}
	}
	return out
}

func init() {
	rules.Register("dart/gate-reads-are-listened", newGateReadsAreListened)
}
