package sqlextract

import (
	"go/ast"
	"sort"
)

// closed_helpers.go — A FUNCTION WHOSE EVERY RETURN IS A CONSTANT IS A CLOSED SET.
//
// A repository keeps an optional clause in a function of the request: when the
// filter is empty it returns nothing, and otherwise the one clause it bound. The
// statement it joins has a known number of shapes, and each is text the author
// wrote. Whatever such a function is passed and however it chooses, it can only
// return one of its constants, so the extractor reads a call to it as those values.
//
// A CLOSED HELPER is a package-level function, not a method, with one unnamed
// string result, whose every return folds to a constant with nothing bound. Any
// parameter types are fine: no return reads one. A return that reads a parameter, a
// local or another function does not fold, so the helper is not closed and the
// composition calling it stays one the rule cannot read.
//
// LINEAR WORLDS, NEVER MULTIPLIED. The file is read with every helper at its first
// value, with every helper at its last, and once more per other value of each helper
// with the rest at their first. fold's foldWorlds bounds itself the same way, for the
// same reason: which combinations are reachable is not a fact this pass has, and the
// cross product is exponential. So every value is judged in a statement, and the
// first and last worlds cover the usual all-off and all-on shapes. A world may be one
// no path produces, which a rule's author clears as they do a foldWorlds one. A rule
// that needs two particular clauses together may not see that pair; that is the
// disclosed limit, the one the assignment fold already has.
//
// ONLY THE FILE BEING READ CHOOSES. A helper called from a package variable, or from
// a template in another file, or from another helper, stays unread: the choice lives
// on the file being read, and reading those at their first value would drop the rest
// without saying so.
//
// PAST THE CAP, NOTHING IS READ. Dropping a world would silence it, so a file needing
// more than maxClosedWorlds goes back to reporting the composition as unread.
const maxClosedWorlds = 256

// closedPass is what one read of a file chose and what it found.
type closedPass struct {
	off   bool           // read no helper at all
	world map[string]int // the helper held at another value, by closedKey
	used  map[string]int // helpers this read called, with their value counts
}

// withClosedWorlds runs extract once per world, keeping the first world's sites and
// adding each later world's candidates it has not seen.
func withClosedWorlds(extract func(*closedPass) ([]Candidate, []Site, error)) ([]Candidate, []Site, error) {
	base := &closedPass{used: map[string]int{}}
	cands, sites, err := extract(base)
	if err != nil || len(base.used) == 0 {
		return cands, sites, err
	}
	keys := make([]string, 0, len(base.used))
	total := 0
	for key, n := range base.used {
		keys = append(keys, key)
		total += n - 1
	}
	if total > maxClosedWorlds {
		return extract(&closedPass{off: true})
	}
	sort.Strings(keys)
	seen := make(map[string]bool, len(cands))
	for _, c := range cands {
		seen[c.Text] = true
	}
	read := func(world map[string]int) error {
		more, _, err := extract(&closedPass{world: world, used: map[string]int{}})
		for _, c := range more {
			if !seen[c.Text] {
				seen[c.Text] = true
				cands = append(cands, c)
			}
		}
		return err
	}
	last := make(map[string]int, len(keys))
	for _, key := range keys {
		last[key] = base.used[key] - 1
	}
	if err := read(last); err != nil {
		return nil, nil, err
	}
	for _, key := range keys {
		for i := 1; i < base.used[key]; i++ {
			if err := read(map[string]int{key: i}); err != nil {
				return nil, nil, err
			}
		}
	}
	return cands, sites, nil
}

// isClosedShape reports whether fd could be a closed helper. Whether its returns
// fold is settled where it is called.
func isClosedShape(fd *ast.FuncDecl) bool {
	if fd == nil || fd.Recv != nil || fd.Type.TypeParams != nil || fd.Body == nil {
		return false
	}
	res := fd.Type.Results
	return res != nil && len(res.List) == 1 && len(res.List[0].Names) == 0 && isIdent(res.List[0].Type, "string")
}

// closedHelper resolves fun to a closed-shaped function, naming it for the world.
func (e *constEnv) closedHelper(fun ast.Expr, cf *constFile) (funcDef, string, bool) {
	var t *pkgTable
	var name string
	switch f := fun.(type) {
	case *ast.Ident:
		if f.Obj != nil {
			if _, ok := f.Obj.Decl.(*ast.FuncDecl); !ok || f.Obj.Kind != ast.Fun {
				return funcDef{}, "", false
			}
		}
		t, name = e.ownTable(cf), f.Name
	case *ast.SelectorExpr:
		x, ok := f.X.(*ast.Ident)
		if !ok || x.Obj != nil || !ast.IsExported(f.Sel.Name) {
			return funcDef{}, "", false
		}
		t, name = e.importTable(cf, x.Name), f.Sel.Name
	}
	if t == nil || t.decls[name] != 1 {
		return funcDef{}, "", false
	}
	def, ok := t.closed[name]
	if !ok {
		return funcDef{}, "", false
	}
	return def, def.file.dir + "." + name, true
}

// closedValues folds fd's returns, in source order and without repeats. ok is false
// when any return does not fold, or a function literal hides what the body returns.
func (e *constEnv) closedValues(def funcDef, depth int) ([]string, bool) {
	var values []string
	seen := map[string]bool{}
	ok := true
	ast.Inspect(def.decl.Body, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			if len(v.Results) != 1 {
				ok = false
				return false
			}
			s, folds := e.fold(v.Results[0], def.file, false, nil, depth+1)
			if !folds {
				ok = false
				return false
			}
			if !seen[s] {
				seen[s] = true
				values = append(values, s)
			}
		}
		return ok
	})
	return values, ok && len(values) > 0
}

// callClosed reads call as the value this read's world holds its helper at.
func (e *constEnv) callClosed(call *ast.CallExpr, cf *constFile, depth int) (string, bool) {
	if e == nil || cf == nil || cf.closed == nil || cf.closed.off {
		return "", false
	}
	def, key, ok := e.closedHelper(call.Fun, cf)
	if !ok {
		return "", false
	}
	values, ok := e.closedValues(def, depth)
	if !ok {
		return "", false
	}
	if cf.closed.used != nil {
		cf.closed.used[key] = len(values)
	}
	if i := cf.closed.world[key]; i < len(values) {
		return values[i], true
	}
	return values[0], true
}
