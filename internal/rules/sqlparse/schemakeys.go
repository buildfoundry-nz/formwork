package sqlparse

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/buildfoundry-nz/formwork/internal/scan"
	pg "github.com/pganalyze/pg_query_go/v6"
)

// schemakeys.go — the relation keys sql/locking-select-order reads from a
// schema file (params.schema).
//
// unique_key_columns is one list of column NAMES for every relation, so it
// cannot say that item_id identifies a row of item_price_overrides
// and not of materials, that (session_id, row_key) is one key, or that
// org_id is unique only where site_id IS NULL. A schema says all three. With
// params.schema set, a relation's keys are its PRIMARY KEY, its UNIQUE
// constraints and its unique indexes, each a column set, and a partial index
// carries its predicate. A key counts for a statement only when the statement
// pins or orders EVERY column of it, and a partial key only when the
// statement's WHERE repeats each conjunct of the index predicate.

// relationKey is one column set that identifies a row of its relation.
// pred is a partial index's predicate as normalised conjuncts; empty for a
// full key. A key column is a plain column, or an index expression, which
// carries the columns it reads in exprs at the same index (nil for a plain
// column): an expression of pinned columns and constants is one value.
type relationKey struct {
	cols  []string
	exprs [][]string
	pred  []string
}

// schemaKeys indexes the keys a schema file declares, by "schema.relation"
// and by bare relation name. A bare name declared in two schemas is
// ambiguous and resolves to no key.
type schemaKeys struct {
	qualified map[string][]relationKey
	bare      map[string][]relationKey
	ambiguous map[string]bool
}

// keysFor returns the keys of the relation rv names.
func (k *schemaKeys) keysFor(rv *pg.RangeVar) []relationKey {
	if rv == nil {
		return nil
	}
	rel := strings.ToLower(rv.GetRelname())
	if s := rv.GetSchemaname(); s != "" {
		return k.qualified[strings.ToLower(s)+"."+rel]
	}
	if k.ambiguous[rel] {
		return nil
	}
	return k.bare[rel]
}

// schemaKeyCache loads a schema file once per walk. Every file a run checks
// shares one *scan.FileSet, so the file is parsed once per run.
type schemaKeyCache struct {
	paths []string
	mu    sync.Mutex
	by    map[*scan.FileSet]*schemaKeysResult
}

type schemaKeysResult struct {
	keys *schemaKeys
	err  error
}

func (c *schemaKeyCache) load(f *scan.File) (*schemaKeys, error) {
	tree := f.Tree()
	if tree == nil {
		return nil, fmt.Errorf("sql/locking-select-order: params.schemas %q: %s has no tree to read them from", c.paths, f.Path())
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if r, ok := c.by[tree]; ok {
		return r.keys, r.err
	}
	keys, err := readSchemaKeys(tree, c.paths)
	if c.by == nil {
		c.by = map[*scan.FileSet]*schemaKeysResult{}
	}
	c.by[tree] = &schemaKeysResult{keys: keys, err: err}
	return keys, err
}

// readSchemaKeys parses the schema files at rels in tree. A missing or
// unparseable file is an error, never an empty key set: an empty set would
// turn every keyed lookup into a finding with no word about why.
func readSchemaKeys(tree *scan.FileSet, rels []string) (*schemaKeys, error) {
	k := &schemaKeys{qualified: map[string][]relationKey{}, bare: map[string][]relationKey{}, ambiguous: map[string]bool{}}
	schemaOf := map[string]string{}
	for _, rel := range rels {
		if err := k.read(tree, rel, schemaOf); err != nil {
			return nil, err
		}
	}
	return k, nil
}

func (k *schemaKeys) read(tree *scan.FileSet, rel string, schemaOf map[string]string) error {
	sf := tree.File(rel)
	if sf == nil {
		return fmt.Errorf("sql/locking-select-order: params.schemas entry %q is not a file in this tree", rel)
	}
	content, err := sf.Content()
	if err != nil {
		return fmt.Errorf("sql/locking-select-order: params.schemas entry %q: %w", rel, err)
	}
	res, err := parse(string(content))
	if err != nil {
		return fmt.Errorf("sql/locking-select-order: params.schemas entry %q does not parse: %w", rel, err)
	}
	add := func(rv *pg.RangeVar, key relationKey) {
		if rv == nil || len(key.cols) == 0 {
			return
		}
		rel := strings.ToLower(rv.GetRelname())
		schema := strings.ToLower(rv.GetSchemaname())
		k.qualified[schema+"."+rel] = append(k.qualified[schema+"."+rel], key)
		if prev, ok := schemaOf[rel]; ok && prev != schema {
			k.ambiguous[rel] = true
		}
		schemaOf[rel] = schema
		k.bare[rel] = append(k.bare[rel], key)
	}
	for _, raw := range res.GetStmts() {
		n := raw.GetStmt()
		switch {
		case n.GetCreateStmt() != nil:
			cs := n.GetCreateStmt()
			for _, elt := range cs.GetTableElts() {
				if cd := elt.GetColumnDef(); cd != nil {
					for _, cn := range cd.GetConstraints() {
						if isKeyConstraint(cn.GetConstraint()) {
							add(cs.GetRelation(), relationKey{cols: []string{strings.ToLower(cd.GetColname())}})
						}
					}
				}
				if con := elt.GetConstraint(); isKeyConstraint(con) {
					add(cs.GetRelation(), relationKey{cols: nameList(con.GetKeys())})
				}
			}
		case n.GetAlterTableStmt() != nil:
			at := n.GetAlterTableStmt()
			for _, cmd := range at.GetCmds() {
				c := cmd.GetAlterTableCmd()
				if c == nil || c.GetSubtype() != pg.AlterTableType_AT_AddConstraint {
					continue
				}
				if con := c.GetDef().GetConstraint(); isKeyConstraint(con) {
					add(at.GetRelation(), relationKey{cols: nameList(con.GetKeys())})
				}
			}
		case n.GetIndexStmt() != nil:
			is := n.GetIndexStmt()
			if !is.GetUnique() {
				continue
			}
			key, ok := indexKey(is.GetIndexParams())
			if !ok {
				continue
			}
			pred, ok := predicateConjuncts(is.GetWhereClause(), "", true)
			if !ok {
				continue // a predicate this file cannot normalise cannot be matched, so the key is unusable
			}
			key.pred = pred
			add(is.GetRelation(), key)
		}
	}
	return nil
}

// indexKey reads a unique index's elements. A column element is a key
// column. An expression element is one too, carrying the columns it reads,
// when it is built only from columns, constants, casts, COALESCE and
// function calls; any other expression makes the index unusable here.
func indexKey(params []*pg.Node) (relationKey, bool) {
	var key relationKey
	for _, p := range params {
		ie := p.GetIndexElem()
		if ie == nil {
			return relationKey{}, false
		}
		if name := ie.GetName(); name != "" {
			key.cols = append(key.cols, strings.ToLower(name))
			key.exprs = append(key.exprs, nil)
			continue
		}
		refs, ok := expressionColumns(ie.GetExpr())
		if !ok || len(refs) == 0 {
			return relationKey{}, false
		}
		key.cols = append(key.cols, "")
		key.exprs = append(key.exprs, refs)
	}
	return key, len(key.cols) > 0
}

// expressionColumns returns the columns an index expression reads, or false
// when it holds a shape whose value is not fixed by those columns.
func expressionColumns(n *pg.Node) ([]string, bool) {
	var refs []string
	ok := true
	var walk func(n *pg.Node)
	walk = func(n *pg.Node) {
		if !ok || n == nil {
			return
		}
		switch {
		case n.GetColumnRef() != nil:
			_, col, good := columnRefParts(n)
			if !good {
				ok = false
				return
			}
			refs = append(refs, strings.ToLower(col))
		case n.GetAConst() != nil:
		case n.GetTypeCast() != nil:
			walk(n.GetTypeCast().GetArg())
		case n.GetCoalesceExpr() != nil:
			for _, a := range n.GetCoalesceExpr().GetArgs() {
				walk(a)
			}
		case n.GetFuncCall() != nil:
			for _, a := range n.GetFuncCall().GetArgs() {
				walk(a)
			}
		default:
			ok = false
		}
	}
	walk(n)
	return refs, ok
}

// isKeyConstraint reports whether con is a PRIMARY KEY or UNIQUE constraint
// over plain columns. UNIQUE USING INDEX names no columns and is read from
// the index instead.
func isKeyConstraint(con *pg.Constraint) bool {
	if con == nil {
		return false
	}
	t := con.GetContype()
	return t == pg.ConstrType_CONSTR_PRIMARY || t == pg.ConstrType_CONSTR_UNIQUE
}

func nameList(nodes []*pg.Node) []string {
	var out []string
	for _, n := range nodes {
		if s := n.GetString_().GetSval(); s != "" {
			out = append(out, strings.ToLower(s))
		}
	}
	return out
}

// predicateConjuncts normalises a conjunction of `col IS [NOT] NULL` and
// `col = <constant>` into sorted strings, keeping only the columns of rel
// (any column when rel is "" and unqualified is true, as in an index
// predicate). ok is false when where holds any other shape, so a partial key
// whose predicate this cannot read is never matched by accident.
func predicateConjuncts(where *pg.Node, rel string, unqualified bool) ([]string, bool) {
	if where == nil {
		return nil, true
	}
	var out []string
	ok := true
	var walk func(n *pg.Node)
	walk = func(n *pg.Node) {
		if !ok || n == nil {
			return
		}
		if b := n.GetBoolExpr(); b != nil {
			if b.GetBoolop() != pg.BoolExprType_AND_EXPR {
				ok = false
				return
			}
			for _, a := range b.GetArgs() {
				walk(a)
			}
			return
		}
		if s, good := conjunctText(n, rel, unqualified); good {
			out = append(out, s)
			return
		}
		ok = false
	}
	walk(where)
	sort.Strings(out)
	return out, ok
}

// conjunctText renders one `col IS [NOT] NULL` or `col = constant` conjunct on
// rel as "col IS NULL", "col IS NOT NULL" or "col = <constant>".
func conjunctText(n *pg.Node, rel string, unqualified bool) (string, bool) {
	if nt := n.GetNullTest(); nt != nil {
		col, ok := relColumn(nt.GetArg(), rel, unqualified)
		if !ok {
			return "", false
		}
		if nt.GetNulltesttype() == pg.NullTestType_IS_NULL {
			return col + " IS NULL", true
		}
		return col + " IS NOT NULL", true
	}
	if a := n.GetAExpr(); a != nil && a.GetKind() == pg.A_Expr_Kind_AEXPR_OP && opName(a) == "=" {
		if col, ok := relColumn(a.GetLexpr(), rel, unqualified); ok {
			if v, ok := constantText(a.GetRexpr()); ok {
				return col + " = " + v, true
			}
		}
		if col, ok := relColumn(a.GetRexpr(), rel, unqualified); ok {
			if v, ok := constantText(a.GetLexpr()); ok {
				return col + " = " + v, true
			}
		}
	}
	return "", false
}

// relColumn returns the lower-cased column node names when it is a column of
// rel: qualified by rel, or unqualified when unqualified is allowed.
func relColumn(node *pg.Node, rel string, unqualified bool) (string, bool) {
	if tc := node.GetTypeCast(); tc != nil {
		return relColumn(tc.GetArg(), rel, unqualified)
	}
	q, col, ok := columnRefParts(node)
	if !ok {
		return "", false
	}
	if q == "" && !unqualified {
		return "", false
	}
	if q != "" && q != rel {
		return "", false
	}
	return strings.ToLower(col), true
}

// constantText renders a literal, unwrapping casts, so 'org'::text and 'org'
// compare equal.
func constantText(node *pg.Node) (string, bool) {
	if tc := node.GetTypeCast(); tc != nil {
		return constantText(tc.GetArg())
	}
	c := node.GetAConst()
	if c == nil {
		return "", false
	}
	switch {
	case c.GetIsnull():
		return "NULL", true
	case c.GetSval() != nil:
		return "'" + c.GetSval().GetSval() + "'", true
	case c.GetIval() != nil:
		return fmt.Sprint(c.GetIval().GetIval()), true
	case c.GetBoolval() != nil:
		return fmt.Sprint(c.GetBoolval().GetBoolval()), true
	case c.GetFval() != nil:
		return c.GetFval().GetFval(), true
	}
	return "", false
}
