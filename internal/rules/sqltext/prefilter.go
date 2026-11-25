package sqltext

import (
	"go/scanner"
	"go/token"
	"strings"
	"sync"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"github.com/buildfoundry-nz/formwork/internal/sqlextract"
)

// nameGate is the template names whose bodies can carry the prefilter literal,
// learned once per walk. A caller of one of those functions folds to text the
// literal may be absent from, so the caller is still parsed.
type nameGate struct {
	mu    sync.Mutex
	tree  *scan.FileSet
	lit   string
	names []string
}

// Prefilter reports the literal gate. An empty gate is not a prefilter
// (rules.PrefilterOf). Folding can assemble the literal from pieces, so this
// checker does not implement PrefilterImplication: implied=true would be a lie.
func (c *predicate) Prefilter() string { return c.prefilter }

// WithoutPrefilter returns the same predicate with the gate removed. The
// compiled regexes are stateless and safe to share. The name cache is shared
// too and unused once the gate is empty.
func (c *predicate) WithoutPrefilter() rules.Checker {
	cp := *c
	cp.prefilter = ""
	return &cp
}

// skipGo reports whether path's bytes cannot produce a statement this rule
// would judge. A file is still parsed when it holds the literal, when it
// composes strings (a '+' chain, a Sprint, or a Builder can assemble the
// literal or hide it in a piece the extractor cannot read), or when it names
// a SQL template whose body contains the literal.
func (c *predicate) skipGo(f *scan.File, content []byte) bool {
	if c.prefilter == "" {
		return false
	}
	s := string(content)
	if strings.Contains(s, c.prefilter) || goComposes(s) {
		return false
	}
	for _, name := range c.templateNames(f) {
		if identContains(s, name) {
			return false
		}
	}
	return true
}

func (c *predicate) templateNames(f *scan.File) []string {
	tree := f.Tree()
	if tree == nil {
		return nil
	}
	c.names.mu.Lock()
	defer c.names.mu.Unlock()
	if c.names.tree == tree && c.names.lit == c.prefilter {
		return c.names.names
	}
	seen := map[string]bool{}
	var names []string
	for _, file := range tree.Files {
		p := file.Path()
		if !strings.HasSuffix(p, ".go") {
			continue
		}
		src, err := file.Content()
		if err != nil || !strings.Contains(string(src), c.prefilter) {
			continue
		}
		for _, name := range sqlextract.TemplateNames(p, src) {
			if name != "" && !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
	}
	c.names.tree = tree
	c.names.lit = c.prefilter
	c.names.names = names
	return names
}

// goComposes reports whether src can assemble SQL out of more than one piece:
// a binary '+', a fmt.Sprint family call, or a strings.Builder. '++' and '+='
// are not concatenation.
func goComposes(src string) bool {
	if strings.Contains(src, "Sprint") || strings.Contains(src, "strings.Builder") || strings.Contains(src, "WriteString") {
		return true
	}
	var sc scanner.Scanner
	fs := token.NewFileSet()
	file := fs.AddFile("", fs.Base(), len(src))
	sc.Init(file, []byte(src), nil, 0)
	for {
		_, tok, _ := sc.Scan()
		if tok == token.EOF {
			return false
		}
		if tok == token.ADD {
			return true
		}
	}
}

func identContains(src, name string) bool {
	rest := src
	for {
		i := strings.Index(rest, name)
		if i < 0 {
			return false
		}
		before := i == 0 || !sqlIdentByte(rest[i-1])
		end := i + len(name)
		after := end == len(rest) || !sqlIdentByte(rest[end])
		if before && after {
			return true
		}
		rest = rest[i+1:]
	}
}
