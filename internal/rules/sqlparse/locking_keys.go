package sqlparse

import (
	"strings"

	pg "github.com/pganalyze/pg_query_go/v6"
)

// locking_keys.go — the verdict sql/locking-select-order reaches from a
// schema's relation keys (params.schema, schemakeys.go).
//
// A locking SELECT is safe from the sibling-order deadlock when every relation
// it locks is either ORDERED BY a whole key, or reduced to one row:
//
//   - ORDER BY covers every column of one key of the relation: the locked rows
//     are visited in one order by every statement of this shape;
//   - the statement pins every column of one key to a single value: the
//     relation contributes one row, so it has no siblings to order. A column is
//     pinned by `col = $1` (or a constant, a cast of one, a function of them),
//     or by an inner-join or WHERE equality to a column of a relation that is
//     itself reduced to one row — `JOIN projects p ON p.current_boq_id = b.id
//     WHERE p.id = $1 FOR UPDATE OF b` locks one boq;
//   - a key column the statement pins also counts toward an ORDER BY: rows
//     sharing project_id = $1 are totally ordered by ordinal when
//     (project_id, ordinal) is a key.
//
// An index expression is a key column whose value its columns fix: it is
// pinned when every column it reads is pinned or tested IS NULL, so
// `org_id = $1 AND site_id IS NULL` pins (org_id, COALESCE(site_id, …)). An
// expression is never an ORDER BY witness, since the sort names a column.
//
// A partial key counts only where the statement's own conditions on the
// relation repeat every conjunct of the index predicate. Conditions are read
// from WHERE and from INNER JOIN quals only, conjunct by conjunct; an OR
// anywhere at the top of either pins nothing from it. An outer join's qual
// pins nothing, since its null-extended side is not restricted by it.

// keyedCompliant reports whether every locked relation of sel is ordered by a
// whole key or reduced to one row.
func keyedCompliant(sel *pg.SelectStmt, keys *schemaKeys) bool {
	locked := lockedRelations(sel)
	if len(locked) == 0 {
		return false
	}
	rels := relationsByID(sel.GetFromClause())
	single := len(rels) == 1
	conds := statementConjuncts(sel)
	pinned, reduced := reducedRelations(rels, conds, keys, single)
	for _, id := range locked {
		rv := rels[id]
		if rv == nil {
			return false
		}
		if reduced[id] {
			continue
		}
		if !sortCoversKey(sel, keys.keysFor(rv), id, conds, pinned[id], single) {
			return false
		}
	}
	return true
}

// relationsByID maps each base relation of a FROM list to its identifier
// (alias, else relation name). Subqueries contribute nothing.
func relationsByID(from []*pg.Node) map[string]*pg.RangeVar {
	out := map[string]*pg.RangeVar{}
	var visit func(n *pg.Node)
	visit = func(n *pg.Node) {
		if n == nil {
			return
		}
		if rv := n.GetRangeVar(); rv != nil {
			out[rangeVarID(rv)] = rv
			return
		}
		if j := n.GetJoinExpr(); j != nil {
			visit(j.GetLarg())
			visit(j.GetRarg())
		}
	}
	for _, n := range from {
		visit(n)
	}
	return out
}

// statementConjuncts returns the top-level conjuncts of sel's WHERE clause and
// of every INNER JOIN qual in its FROM list. A clause whose top is not a
// conjunction contributes nothing.
func statementConjuncts(sel *pg.SelectStmt) []*pg.Node {
	var out []*pg.Node
	out = append(out, splitConjuncts(sel.GetWhereClause())...)
	var visit func(n *pg.Node)
	visit = func(n *pg.Node) {
		j := n.GetJoinExpr()
		if j == nil {
			return
		}
		visit(j.GetLarg())
		visit(j.GetRarg())
		if j.GetJointype() == pg.JoinType_JOIN_INNER {
			out = append(out, splitConjuncts(j.GetQuals())...)
		}
	}
	for _, n := range sel.GetFromClause() {
		visit(n)
	}
	return out
}

// splitConjuncts flattens nested ANDs. Anything else at the top — OR, NOT —
// yields no conjuncts: none of its parts holds on every row.
func splitConjuncts(n *pg.Node) []*pg.Node {
	if n == nil {
		return nil
	}
	b := n.GetBoolExpr()
	if b == nil {
		return []*pg.Node{n}
	}
	if b.GetBoolop() != pg.BoolExprType_AND_EXPR {
		return nil
	}
	var out []*pg.Node
	for _, a := range b.GetArgs() {
		if a.GetBoolExpr() != nil && a.GetBoolExpr().GetBoolop() != pg.BoolExprType_AND_EXPR {
			continue
		}
		out = append(out, splitConjuncts(a)...)
	}
	return out
}

// reducedRelations returns, per relation, the columns the conditions pin (a
// column tested IS NULL counts as pinned to NULL) and the relations they reduce
// to one row, iterating to a fixpoint: a relation pinned on a whole key is
// reduced, and an equality between its column and another relation's column
// pins that column.
func reducedRelations(rels map[string]*pg.RangeVar, conds []*pg.Node, keys *schemaKeys, single bool) (map[string]map[string]bool, map[string]bool) {
	pinned := map[string]map[string]bool{}
	pin := func(id, col string) bool {
		if pinned[id] == nil {
			pinned[id] = map[string]bool{}
		}
		if pinned[id][col] {
			return false
		}
		pinned[id][col] = true
		return true
	}
	for _, n := range conds {
		for id := range rels {
			if col, ok := pinnedColumn(n, id, single); ok {
				pin(id, col)
			}
			if nt := n.GetNullTest(); nt != nil && nt.GetNulltesttype() == pg.NullTestType_IS_NULL {
				if col, ok := relColumn(nt.GetArg(), id, single); ok {
					pin(id, col)
				}
			}
		}
	}
	reduced := map[string]bool{}
	for changed := true; changed; {
		changed = false
		for id, rv := range rels {
			if reduced[id] {
				continue
			}
			for _, k := range keys.keysFor(rv) {
				if keyCovered(k, pinned[id], nil) && predicateHolds(k, conds, id, single) {
					reduced[id] = true
					changed = true
					break
				}
			}
		}
		for _, n := range conds {
			l, r, ok := columnEquality(n)
			if !ok {
				continue
			}
			if reduced[r.rel] && rels[l.rel] != nil && pin(l.rel, l.col) {
				changed = true
			}
			if reduced[l.rel] && rels[r.rel] != nil && pin(r.rel, r.col) {
				changed = true
			}
		}
	}
	return pinned, reduced
}

// pinnedColumn reports the column of relation id that n pins to a single
// value: `<id>.col = <single value>` or its mirror. An unqualified column
// belongs to id only when id is the sole relation.
func pinnedColumn(n *pg.Node, id string, single bool) (string, bool) {
	a := n.GetAExpr()
	if a == nil || a.GetKind() != pg.A_Expr_Kind_AEXPR_OP || opName(a) != "=" {
		return "", false
	}
	if col, ok := relColumn(a.GetLexpr(), id, single); ok && pinsSingleValue(a.GetRexpr()) {
		return col, true
	}
	if col, ok := relColumn(a.GetRexpr(), id, single); ok && pinsSingleValue(a.GetLexpr()) {
		return col, true
	}
	return "", false
}

type qualifiedColumn struct{ rel, col string }

// columnEquality reports `a.x = b.y` between two qualified columns.
func columnEquality(n *pg.Node) (qualifiedColumn, qualifiedColumn, bool) {
	a := n.GetAExpr()
	if a == nil || a.GetKind() != pg.A_Expr_Kind_AEXPR_OP || opName(a) != "=" {
		return qualifiedColumn{}, qualifiedColumn{}, false
	}
	lq, lc, lok := castColumnParts(a.GetLexpr())
	rq, rc, rok := castColumnParts(a.GetRexpr())
	if !lok || !rok || lq == "" || rq == "" {
		return qualifiedColumn{}, qualifiedColumn{}, false
	}
	return qualifiedColumn{lq, lc}, qualifiedColumn{rq, rc}, true
}

func castColumnParts(n *pg.Node) (string, string, bool) {
	if tc := n.GetTypeCast(); tc != nil {
		return castColumnParts(tc.GetArg())
	}
	q, c, ok := columnRefParts(n)
	if !ok {
		return "", "", false
	}
	return q, strings.ToLower(c), true
}

// predicateHolds reports whether the conditions on relation id repeat every
// conjunct of k's partial predicate. A full key holds trivially.
func predicateHolds(k relationKey, conds []*pg.Node, id string, single bool) bool {
	if len(k.pred) == 0 {
		return true
	}
	have := map[string]bool{}
	for _, n := range conds {
		if s, ok := conjunctText(n, id, single); ok {
			have[s] = true
		}
	}
	for _, p := range k.pred {
		if !have[p] {
			return false
		}
	}
	return true
}

// sortCoversKey reports whether every column of one of keys is either named by
// sel's ORDER BY as a column of relation id or pinned by the statement.
func sortCoversKey(sel *pg.SelectStmt, keys []relationKey, id string, conds []*pg.Node, pinned map[string]bool, single bool) bool {
	if len(sel.GetSortClause()) == 0 {
		return false
	}
	sorted := map[string]bool{}
	targets := sel.GetTargetList()
	for _, node := range sel.GetSortClause() {
		sb := node.GetSortBy()
		if sb == nil {
			continue
		}
		item, ok := resolveSortItem(sb.GetNode(), targets)
		if !ok {
			continue
		}
		if col, ok := relColumn(item, id, single); ok {
			sorted[col] = true
		}
	}
	for _, k := range keys {
		if keyCovered(k, pinned, sorted) && predicateHolds(k, conds, id, single) {
			return true
		}
	}
	return false
}

// keyCovered reports whether every column of k is pinned or, for a plain
// column, sorted on. An expression column is covered when every column it
// reads is pinned.
func keyCovered(k relationKey, pinned, sorted map[string]bool) bool {
	if len(k.cols) == 0 {
		return false
	}
	for i, c := range k.cols {
		if i < len(k.exprs) && k.exprs[i] != nil {
			for _, ref := range k.exprs[i] {
				if !pinned[ref] {
					return false
				}
			}
			continue
		}
		if !pinned[c] && !sorted[c] {
			return false
		}
	}
	return true
}
