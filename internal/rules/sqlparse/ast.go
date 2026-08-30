package sqlparse

import (
	"strings"

	pg "github.com/pganalyze/pg_query_go/v6"
)

// lockingSelects returns every SelectStmt carrying a non-empty LockingClause
// reachable from node: the statement itself, an INSERT's SELECT source, CTEs in
// a WITH clause attached to EITHER a SELECT or an INSERT, set-operation arms,
// and a data-modifying CTE whose own query is an INSERT...SELECT. This covers
// the idiomatic lock-and-return shapes [R7]. Known remaining gap: a locking
// SELECT buried in a scalar/FROM sublink inside an expression is not walked.
func lockingSelects(node *pg.Node) []*pg.SelectStmt {
	var out []*pg.SelectStmt
	var visitSelect func(sel *pg.SelectStmt)
	var visitCTEs func(wc *pg.WithClause)
	visitCTEs = func(wc *pg.WithClause) {
		if wc == nil {
			return
		}
		for _, cte := range wc.GetCtes() {
			e := cte.GetCommonTableExpr()
			if e == nil {
				continue
			}
			q := e.GetCtequery()
			visitSelect(q.GetSelectStmt())
			// data-modifying CTE: the CTE's own query is an INSERT...SELECT
			if ins := q.GetInsertStmt(); ins != nil {
				visitCTEs(ins.GetWithClause())
				visitSelect(ins.GetSelectStmt().GetSelectStmt())
			}
		}
	}
	visitSelect = func(sel *pg.SelectStmt) {
		if sel == nil {
			return
		}
		if len(sel.GetLockingClause()) > 0 {
			out = append(out, sel)
		}
		visitCTEs(sel.GetWithClause())
		visitSelect(sel.GetLarg()) // set-op arms (UNION/INTERSECT/EXCEPT)
		visitSelect(sel.GetRarg())
	}
	switch {
	case node.GetSelectStmt() != nil:
		visitSelect(node.GetSelectStmt())
	case node.GetInsertStmt() != nil:
		// An INSERT can carry its OWN WITH clause (CTEs feeding the insert) plus
		// a SELECT source. InsertStmt.GetSelectStmt() returns *pg.Node, so unwrap
		// once more to reach the *SelectStmt.
		ins := node.GetInsertStmt()
		visitCTEs(ins.GetWithClause())
		visitSelect(ins.GetSelectStmt().GetSelectStmt())
	case node.GetUpdateStmt() != nil:
		// UPDATE carries no SELECT source of its own, but its WITH clause can
		// contain a locking SELECT feeding the UPDATE's WHERE/FROM.
		visitCTEs(node.GetUpdateStmt().GetWithClause())
	case node.GetDeleteStmt() != nil:
		// Same shape as UPDATE: DELETE has no SELECT source, only a WITH clause.
		visitCTEs(node.GetDeleteStmt().GetWithClause())
	}
	return out
}

// allLocksSkip reports whether every locking clause on sel uses SKIP LOCKED
// (LockWaitSkip). A SKIP-LOCKED lock never waits on a row another transaction
// holds — it skips it — so it can never be the waiting edge in a lock-wait
// cycle and cannot deadlock, whatever its ORDER BY (#41). Only SKIP LOCKED
// qualifies: the blocking default waits, and NOWAIT (LockWaitError) errors
// rather than waits — a non-deadlocking policy whose reasoning differs and is
// handled separately. A statement mixing a SKIP-LOCKED clause with a blocking
// one still has a clause that can wait, so it is not exempt.
func allLocksSkip(sel *pg.SelectStmt) bool {
	clauses := sel.GetLockingClause()
	if len(clauses) == 0 {
		return false
	}
	for _, lcNode := range clauses {
		lc := lcNode.GetLockingClause()
		if lc == nil || lc.GetWaitPolicy() != pg.LockWaitPolicy_LockWaitSkip {
			return false
		}
	}
	return true
}

// rangeVarID returns the identifier a RangeVar's columns are qualified by: its
// alias if present, else its relation name.
func rangeVarID(rv *pg.RangeVar) string {
	if a := rv.GetAlias(); a != nil && a.GetAliasname() != "" {
		return a.GetAliasname()
	}
	return rv.GetRelname()
}

// lockedRelations returns the identifiers (alias or relname) of the relations
// sel's LockingClause locks. FOR UPDATE OF a,b ⇒ the named refs; a bare
// FOR UPDATE (no OF) ⇒ every base relation in the FROM clause.
func lockedRelations(sel *pg.SelectStmt) []string {
	var named []string
	for _, lcNode := range sel.GetLockingClause() {
		lc := lcNode.GetLockingClause()
		if lc == nil {
			continue
		}
		for _, rel := range lc.GetLockedRels() {
			if rv := rel.GetRangeVar(); rv != nil {
				named = append(named, rangeVarID(rv))
			}
		}
	}
	if len(named) > 0 {
		return named
	}
	return baseRelationIDs(sel.GetFromClause())
}

// baseRelationIDs returns the identifier of every base relation (RangeVar) in a
// FROM list, descending JoinExpr arms. Subqueries contribute no identifier.
func baseRelationIDs(from []*pg.Node) []string {
	var ids []string
	var visit func(n *pg.Node)
	visit = func(n *pg.Node) {
		if n == nil {
			return
		}
		if rv := n.GetRangeVar(); rv != nil {
			ids = append(ids, rangeVarID(rv))
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
	return ids
}

// whereIsSingleRowKeyLookup reports whether where pins the LOCKED relation's
// unique key by a conjunctive equality — an AEXPR_OP "=" on a uniqueCols column
// of lockedRel, with no OR/NOT reachable from the top. An unqualified column is
// accepted only when the locked relation is the sole relation in scope.
func whereIsSingleRowKeyLookup(where *pg.Node, uniqueCols map[string]bool, lockedRel string, singleRelation bool) bool {
	if where == nil {
		return false
	}
	found := false
	ok := true
	var walk func(n *pg.Node)
	walk = func(n *pg.Node) {
		if n == nil || !ok {
			return
		}
		if b := n.GetBoolExpr(); b != nil {
			if b.GetBoolop() != pg.BoolExprType_AND_EXPR {
				ok = false // OR / NOT breaks the conjunctive single-row shape
				return
			}
			for _, arg := range b.GetArgs() {
				walk(arg)
			}
			return
		}
		if isLockedKeyEquality(n, uniqueCols, lockedRel, singleRelation) {
			found = true
		}
	}
	walk(where)
	return ok && found
}

// isLockedKeyEquality reports whether node is `<lockedRel>.<uniqueCol> = <param/literal>`
// (or the mirror) using the plain equality operator (AEXPR_OP) — NOT = ANY / IN /
// BETWEEN — where the column belongs to the locked relation AND the other side
// is a parameter or literal constant. `<key> = <other column>` is NOT a
// single-row pin (the other column can vary per row) and must not qualify.
func isLockedKeyEquality(node *pg.Node, uniqueCols map[string]bool, lockedRel string, singleRelation bool) bool {
	a := node.GetAExpr()
	if a == nil || a.GetKind() != pg.A_Expr_Kind_AEXPR_OP {
		return false
	}
	if opName(a) != "=" {
		return false
	}
	lexpr, rexpr := a.GetLexpr(), a.GetRexpr()
	if qualifiesLockedKey(lexpr, uniqueCols, lockedRel, singleRelation) && pinsSingleValue(rexpr) {
		return true
	}
	return qualifiesLockedKey(rexpr, uniqueCols, lockedRel, singleRelation) && pinsSingleValue(lexpr)
}

// pinsSingleValue reports whether node is one value for the whole statement, as
// opposed to another column whose value varies per row. The accepted shapes:
//
//   - a bind parameter ($1) or a literal constant — the base cases;
//   - a TypeCast, unwrapped to its argument recursively ($1::uuid, 5::bigint):
//     a cast does not change whether the underlying value varies;
//   - a function call whose every argument itself pins a single value
//     (decode_id($1), current_setting('app.tenant'), now()). The per-argument
//     check is what keeps coalesce(owner_id, 0) — which varies per row through
//     its column argument — out;
//   - a scalar subquery, `id = (SELECT ...)`, which yields at most one row.
//
// Residual, in keeping with the exemption being a documented heuristic: a
// CORRELATED scalar subquery (one referencing the outer query's columns) does
// vary per row, and is not distinguished here — detecting correlation needs
// outer-reference resolution the rule does not do. Such a subquery keyed on the
// locked relation's own unique key is exotic enough to accept.
func pinsSingleValue(node *pg.Node) bool {
	if node == nil {
		return false
	}
	if tc := node.GetTypeCast(); tc != nil {
		return pinsSingleValue(tc.GetArg()) // unwrap $1::uuid, 5::bigint, etc.
	}
	if fc := node.GetFuncCall(); fc != nil {
		for _, arg := range fc.GetArgs() {
			if !pinsSingleValue(arg) {
				return false
			}
		}
		return true
	}
	if sl := node.GetSubLink(); sl != nil {
		return sl.GetSubLinkType() == pg.SubLinkType_EXPR_SUBLINK
	}
	return node.GetParamRef() != nil || node.GetAConst() != nil
}

// opName returns the operator name of an A_Expr (e.g. "="), or "".
func opName(a *pg.A_Expr) string {
	names := a.GetName()
	if len(names) == 0 {
		return ""
	}
	return names[len(names)-1].GetString_().GetSval()
}

// qualifiesLockedKey reports whether node is a ColumnRef naming a uniqueCols
// column of the locked relation (qualified by lockedRel, or unqualified when the
// locked relation is the only relation in scope). The column name is compared
// case-insensitively: PostgreSQL folds unquoted identifiers to lowercase, so
// the parsed AST always has a lowercase name even when the source SQL (and a
// config's unique_key_columns entry) used mixed case.
func qualifiesLockedKey(node *pg.Node, uniqueCols map[string]bool, lockedRel string, singleRelation bool) bool {
	qualifier, column, ok := columnRefParts(node)
	if !ok || !uniqueCols[strings.ToLower(column)] {
		return false
	}
	if qualifier == "" {
		return singleRelation
	}
	return qualifier == lockedRel
}

// sortHasUniqueKey reports whether any ORDER BY item is a total-order witness
// for the locked relation: either a ColumnRef naming a uniqueCols column of the
// locked relation (qualified by lockedRel, or unqualified when it is the only
// relation in scope — the same rule qualifiesLockedKey applies to the WHERE
// clause), or an item that resolves, via sel's target list, to such a column.
// Two resolvable forms, matching PostgreSQL's own ORDER BY name resolution:
// an integer ordinal (ORDER BY 1) and an output-column alias (SELECT id AS pk
// … ORDER BY pk). [R6]
func sortHasUniqueKey(sel *pg.SelectStmt, uniqueCols map[string]bool, lockedRel string, singleRelation bool) bool {
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
		if qualifiesLockedKey(item, uniqueCols, lockedRel, singleRelation) {
			return true
		}
	}
	return false
}

// sortIsTotalOrder reports whether sel's ORDER BY establishes a total order
// over the whole locked set: EVERY locked relation must have a unique-key
// witness in the sort clause. Locking two relations and ordering by only one
// of their keys still leaves the other's rows visited in an arbitrary order,
// which is the lock cycle this rule exists to prevent — but ordering by both
// (ORDER BY o.id, c.id under a bare FOR UPDATE over a join) is a genuine total
// order and must not be flagged.
//
// An empty locked set (a bare FOR UPDATE whose FROM has no base relation, e.g.
// only subqueries) has no relation to witness and is never a total order.
func sortIsTotalOrder(sel *pg.SelectStmt, uniqueCols map[string]bool, locked []string, singleRelation bool) bool {
	if len(locked) == 0 {
		return false
	}
	for _, rel := range locked {
		if !sortHasUniqueKey(sel, uniqueCols, rel, singleRelation) {
			return false
		}
	}
	return true
}

// resolveSortItem maps one ORDER BY item to the expression it actually sorts
// on. An integer ordinal (ORDER BY 2) and a bare name matching an output-column
// alias both resolve through the target list; anything else is itself. ok is
// false when the item names a target list entry that cannot be resolved (an
// out-of-range ordinal), which is not a witness for anything.
//
// A QUALIFIED name (o.id) is never an output alias — it names a relation column
// directly — so it is returned untouched. Alias resolution takes precedence
// over a same-named relation column, which is PostgreSQL's own rule for a bare
// name in ORDER BY.
func resolveSortItem(item *pg.Node, targets []*pg.Node) (*pg.Node, bool) {
	if ac := item.GetAConst(); ac != nil && ac.GetIval() != nil {
		n := int(ac.GetIval().GetIval())
		if n < 1 || n > len(targets) {
			return nil, false // out-of-range ordinal: not a resolvable witness
		}
		rt := targets[n-1].GetResTarget()
		if rt == nil {
			return nil, false
		}
		return rt.GetVal(), true
	}
	if qualifier, column, ok := columnRefParts(item); ok && qualifier == "" {
		for _, t := range targets {
			rt := t.GetResTarget()
			if rt == nil || rt.GetName() == "" {
				continue
			}
			if strings.EqualFold(rt.GetName(), column) {
				return rt.GetVal(), true
			}
		}
	}
	return item, true
}

// columnRefParts splits a ColumnRef node into its qualifier (the table/alias, the
// second-to-last field, empty if unqualified) and column (the last field). ok is
// false when node is not a ColumnRef or the column is not a plain name (e.g. *).
func columnRefParts(node *pg.Node) (qualifier, column string, ok bool) {
	if node == nil {
		return "", "", false
	}
	cr := node.GetColumnRef()
	if cr == nil {
		return "", "", false
	}
	fields := cr.GetFields()
	if len(fields) == 0 {
		return "", "", false
	}
	column = fields[len(fields)-1].GetString_().GetSval()
	if column == "" {
		return "", "", false
	}
	if len(fields) >= 2 {
		qualifier = fields[len(fields)-2].GetString_().GetSval()
	}
	return qualifier, column, true
}
