package goast

import (
	"fmt"
	"go/ast"
	"go/token"
	"regexp"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"gopkg.in/yaml.v3"
)

// go/expected-derives-from-actual — a conformance test whose EXPECTED value is
// derived from the very artifact it is checking (#3).
//
// Such a test compares an artifact to itself. It can only ever falsify the
// round trip — ordering, dedup, formatting — never agreement with whatever the
// artifact is supposed to track. Two live members were found downstream, and
// they were the only two conformance tests in their package; both headers
// claimed agreement with a writer they never consulted, and with a stale
// registry in place both reported ok while the writer reported it would rewrite
// the file.
//
// WHY THIS CANNOT BE A PATTERN RULE. The correct form and the defective form
// are the same TEXT. A golden-file test — read the committed file, compare it
// to freshly rendered output — is the RIGHT shape, and both spell
// bytes.Equal(os.ReadFile(p), render(x)). The only thing separating them is
// whether the expected side's data traces back to the same read, which is a
// local dataflow fact with no textual signature. Measured downstream, a grep
// for the shape returned 20 candidates, nearly all correct golden tests.
//
// THE TRIGGER IS THE UNDECLARED SELF-COMPARISON, NOT THE SHAPE. The first
// design flagged the shape, and a prototype proved that wrong: after both real
// members were corrected — headers, names and failure messages narrowed to say
// render-normalisation — it still fired on both. Correct behaviour, wrong rule.
// A round-trip normalisation check is legitimate and worth pinning; it is what
// keeps a large generated file diffable. So the rule asks for a DECLARATION,
// in the spirit of `# glob-dead:`, and clears any function that carries one.
type expectedDerivesParams struct {
	Reader  string `yaml:"reader"`  // whole-file read, e.g. ^os\.ReadFile$
	Loader  string `yaml:"loader"`  // loader that returns parsed artifact content
	Compare string `yaml:"compare"` // the comparison the test asserts on
	Declare string `yaml:"declare"` // comment marker that declares a deliberate round trip
}

type expectedDerives struct {
	reader  *regexp.Regexp
	loader  *regexp.Regexp
	compare *regexp.Regexp
	declare string
}

func newExpectedDerives(node *yaml.Node) (rules.Checker, error) {
	var p expectedDerivesParams
	if err := rules.DecodeParams(node, &p); err != nil {
		return nil, err
	}
	const id = "go/expected-derives-from-actual"
	reader, err := compileRe(id, "reader", p.Reader)
	if err != nil {
		return nil, err
	}
	loader, err := compileOptRe(id, "loader", p.Loader)
	if err != nil {
		return nil, err
	}
	compare, err := compileRe(id, "compare", p.Compare)
	if err != nil {
		return nil, err
	}
	if p.Declare == "" {
		// Without a way to declare a legitimate round trip the rule has no cure
		// except deleting a correct test, which is the failure mode the design
		// correction exists to avoid.
		return nil, fmt.Errorf("%s: params.declare must name the comment marker that declares a deliberate self-comparison", id)
	}
	return &expectedDerives{reader: reader, loader: loader, compare: compare, declare: p.Declare}, nil
}

// origin is a data root: either a read site or a local variable that no traced
// expression produced.
type origin string

// originSet is the set of roots an expression's value can be traced to.
type originSet map[origin]bool

func (s originSet) add(o origin) { s[o] = true }
func (s originSet) union(t originSet) {
	for o := range t {
		s[o] = true
	}
}
func (s originSet) intersects(t originSet) bool {
	for o := range s {
		if t[o] {
			return true
		}
	}
	return false
}

// readSite is one reader/loader call and the artifact it points at.
type readSite struct {
	id       origin
	artifact originSet // roots the read's ARGUMENTS trace to
}

func (c *expectedDerives) CheckFile(f *scan.File) ([]rules.Match, error) {
	fset, file, ok, err := parseGoWithComments(f)
	if err != nil || !ok {
		return nil, err
	}
	ib := buildImportBindings(file)

	var ms []rules.Match
	for _, decl := range file.Decls {
		fd, okDecl := decl.(*ast.FuncDecl)
		if !okDecl || fd.Body == nil {
			continue
		}
		if c.declaredIn(file, fd, fset) {
			continue
		}
		ms = append(ms, c.checkFunc(fset, fd, ib)...)
	}
	return ms, nil
}

// declaredIn reports whether the function carries the declaration marker in a
// comment attached to it or inside its body.
func (c *expectedDerives) declaredIn(file *ast.File, fd *ast.FuncDecl, fset *token.FileSet) bool {
	if fd.Doc != nil {
		for _, cm := range fd.Doc.List {
			if containsMarker(cm.Text, c.declare) {
				return true
			}
		}
	}
	lo, hi := fset.Position(fd.Pos()).Line, fset.Position(fd.End()).Line
	for _, cg := range file.Comments {
		for _, cm := range cg.List {
			ln := fset.Position(cm.Pos()).Line
			if ln >= lo && ln <= hi && containsMarker(cm.Text, c.declare) {
				return true
			}
		}
	}
	return false
}

func containsMarker(text, marker string) bool {
	return len(marker) > 0 && len(text) >= len(marker) && indexOf(text, marker) >= 0
}

func indexOf(hay, needle string) int {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

// checkFunc is the dataflow. It walks the body in source order, building the
// origin set of every local, then flags a comparison whose two sides trace to
// reads of the SAME artifact.
func (c *expectedDerives) checkFunc(fset *token.FileSet, fd *ast.FuncDecl, ib importBindings) []rules.Match {
	// PARAMETERS ARE NOT ORIGINS, and the exclusion is derived from the
	// signature rather than a name list. *testing.T reaches every helper in a
	// body; counting a parameter as an origin makes two unrelated roots share
	// one, and then every comparison in every test reads as a self-comparison.
	params := map[string]bool{}
	for _, fl := range fd.Type.Params.List {
		for _, n := range fl.Names {
			params[n.Name] = true
		}
	}

	vars := map[string]originSet{}
	reads := map[origin]readSite{}
	nextRead := 0

	// originsOf traces an expression to its roots.
	var originsOf func(e ast.Expr) originSet
	originsOf = func(e ast.Expr) originSet {
		out := originSet{}
		switch v := e.(type) {
		case nil:
			return out
		case *ast.Ident:
			if params[v.Name] {
				return out // a parameter is not a root
			}
			if s, ok := vars[v.Name]; ok {
				out.union(s)
			}
			return out
		case *ast.CallExpr:
			sel := renderSelector(v.Fun, ib, nil)
			isRead := c.reader.MatchString(sel) || (c.loader != nil && c.loader.MatchString(sel))
			// A CALL'S Fun IS NOT A DATA ORIGIN. Counting the function name
			// makes every filepath.Join share an origin with every other and
			// collapses the analysis. Only the ARGUMENTS carry data.
			args := originSet{}
			for _, a := range v.Args {
				args.union(originsOf(a))
			}
			if isRead {
				id := origin(fmt.Sprintf("read#%d", nextRead))
				nextRead++
				reads[id] = readSite{id: id, artifact: args}
				out.add(id)
				return out
			}
			out.union(args)
			return out
		case *ast.BinaryExpr:
			out.union(originsOf(v.X))
			out.union(originsOf(v.Y))
			return out
		case *ast.UnaryExpr:
			out.union(originsOf(v.X))
			return out
		case *ast.ParenExpr:
			out.union(originsOf(v.X))
			return out
		case *ast.SelectorExpr:
			out.union(originsOf(v.X))
			return out
		case *ast.IndexExpr:
			out.union(originsOf(v.X))
			return out
		case *ast.SliceExpr:
			out.union(originsOf(v.X))
			return out
		case *ast.StarExpr:
			out.union(originsOf(v.X))
			return out
		case *ast.TypeAssertExpr:
			out.union(originsOf(v.X))
			return out
		case *ast.CompositeLit:
			for _, el := range v.Elts {
				out.union(originsOf(el))
			}
			return out
		}
		return out
	}

	var ms []rules.Match
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.AssignStmt:
			// Right-hand sides first, so a var never traces to itself.
			rhs := originSet{}
			for _, r := range v.Rhs {
				rhs.union(originsOf(r))
			}
			for _, l := range v.Lhs {
				id, ok := l.(*ast.Ident)
				if !ok || id.Name == "_" {
					continue
				}
				s := originSet{}
				s.union(rhs)
				if len(s) == 0 {
					// A VARIABLE WHOSE RIGHT-HAND SIDE YIELDS NO OPERAND
					// ORIGINS IS ITS OWN ROOT. Without this, two unrelated
					// locals both trace to the empty set and every comparison
					// between them reads as unrelated — or, worse, as shared.
					s.add(origin("var:" + id.Name))
				}
				vars[id.Name] = s
			}
		case *ast.CallExpr:
			sel := renderSelector(v.Fun, ib, nil)
			if !c.compare.MatchString(sel) || len(v.Args) < 2 {
				return true
			}
			a, b := originsOf(v.Args[0]), originsOf(v.Args[1])
			if site := sharedArtifact(a, b, reads); site != "" {
				ms = append(ms, rules.Match{
					Line: fset.Position(v.Pos()).Line,
					Message: fmt.Sprintf(
						"in func %q: the expected side of %s derives from the same artifact the actual side reads, "+
							"so this compares the artifact to itself — declare it with %q if the round trip is the point",
						fd.Name.Name, sel, c.declare),
				})
			}
		}
		return true
	})
	return ms
}

// sharedArtifact reports a read whose artifact both sides trace to. Two reads
// count as the same artifact when the roots their ARGUMENTS trace to overlap —
// which is how a path built with filepath.Join from the same root is recognised
// as the same file as the one a loader was pointed at.
func sharedArtifact(a, b originSet, reads map[origin]readSite) origin {
	for ra := range a {
		sa, ok := reads[ra]
		if !ok {
			continue
		}
		for rb := range b {
			sb, ok := reads[rb]
			if !ok {
				continue
			}
			if ra == rb || sa.artifact.intersects(sb.artifact) {
				return ra
			}
		}
	}
	return ""
}
