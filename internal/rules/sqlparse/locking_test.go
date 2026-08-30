package sqlparse_test

import "testing"

func TestLockingUnorderedFires(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	sql := "SELECT * FROM t WHERE status = 'x' FOR UPDATE;\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 1 {
		t.Fatalf("unordered sibling-row lock must fire: %+v", ms)
	}
}

func TestLockingOrderedPasses(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	sql := "SELECT * FROM t WHERE status = 'x' ORDER BY id FOR UPDATE;\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 0 {
		t.Fatalf("ordered lock must pass: %+v", ms)
	}
}

func TestLockingNonLockingSelectIgnored(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	sql := "SELECT * FROM t WHERE status = 'x';\n" // no FOR UPDATE
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 0 {
		t.Fatalf("a non-locking SELECT is out of scope: %+v", ms)
	}
}

func TestLockingInCTESelected(t *testing.T) {
	// [R7] the lock is nested in a CTE; must still be selected and flagged.
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	sql := "WITH x AS (SELECT * FROM t WHERE status='x' FOR UPDATE) SELECT * FROM x;\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 1 {
		t.Fatalf("CTE-nested unordered lock must fire: %+v", ms)
	}
}

func TestLockingInInsertSelectSelected(t *testing.T) {
	// [R7] INSERT ... SELECT ... FOR UPDATE.
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	sql := "INSERT INTO log SELECT * FROM t WHERE status='x' FOR UPDATE;\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 1 {
		t.Fatalf("INSERT...SELECT...FOR UPDATE must fire: %+v", ms)
	}
}

func TestLockingInCTEFeedingInsertSelected(t *testing.T) {
	// [R7] the lock is in a CTE that feeds an INSERT: WITH attaches to the INSERT,
	// not the inner SELECT. Must still be selected and flagged.
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	sql := "WITH cte AS (SELECT * FROM t WHERE status='x' FOR UPDATE) INSERT INTO log SELECT * FROM cte;\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 1 {
		t.Fatalf("locking SELECT in a CTE feeding an INSERT must fire: %+v", ms)
	}
}

func TestLockingInDataModifyingCTESelected(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	sql := "WITH moved AS (INSERT INTO archive SELECT * FROM t WHERE status='x' FOR UPDATE RETURNING id) SELECT * FROM moved;\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 1 {
		t.Fatalf("locking SELECT source of a data-modifying CTE must fire: %+v", ms)
	}
}

func TestLockingRejectsUnknownParam(t *testing.T) {
	factory, _ := getFactory(t, "sql/locking-select-order")
	if _, err := factory(paramsNode(t, "bogus: 1\n")); err == nil {
		t.Fatal("unknown param must be rejected")
	}
}

func TestLockingSingleRowPKPasses(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	sql := "SELECT selection_mode FROM t WHERE id = $1 FOR UPDATE;\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 0 {
		t.Fatalf("single-row PK lock must be exempt: %+v", ms)
	}
}

func TestLockingEqAnyFires(t *testing.T) {
	// [R5] id = ANY($1) locks many rows; the "=" operator name must NOT exempt it.
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	sql := "SELECT * FROM t WHERE id = ANY($1) FOR UPDATE;\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 1 {
		t.Fatalf("id = ANY(...) is multi-row and must fire: %+v", ms)
	}
}

func TestLockingOrPredicateFires(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	sql := "SELECT * FROM t WHERE id = $1 OR status = 'x' FOR UPDATE;\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 1 {
		t.Fatalf("an OR predicate is not a single-row lookup: %+v", ms)
	}
}

func TestLockingSingleRowWithAndPasses(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	sql := "SELECT * FROM t WHERE id = $1 AND status = 'x' FOR UPDATE;\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 0 {
		t.Fatalf("id = $1 AND ... is still a single-row lookup: %+v", ms)
	}
}

func TestLockingMultiTableBareForUpdateFires(t *testing.T) {
	// [R3] bare FOR UPDATE over a join has empty LockedRels yet locks BOTH
	// tables; must not be exempted as single-row.
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	sql := "SELECT * FROM a JOIN b ON b.a_id = a.id WHERE a.id = $1 FOR UPDATE;\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 1 {
		t.Fatalf("multi-table bare FOR UPDATE must fire: %+v", ms)
	}
}

func TestLockingForUpdateOfSingleRelationPasses(t *testing.T) {
	// FOR UPDATE OF a locks exactly a; a.id = $1 makes it single-row.
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	sql := "SELECT * FROM a JOIN b ON b.a_id = a.id WHERE a.id = $1 FOR UPDATE OF a;\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 0 {
		t.Fatalf("FOR UPDATE OF a with a.id=$1 is single-row: %+v", ms)
	}
}

func TestLockingCrossTableKeyFires(t *testing.T) {
	// FOR UPDATE OF oi locks the many-side; WHERE pins the OTHER table's key.
	// Locks every order_items row for the order — a multi-row lock that must fire.
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	sql := "SELECT * FROM orders o JOIN order_items oi ON oi.order_id = o.id WHERE o.id = $1 FOR UPDATE OF oi;\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 1 {
		t.Fatalf("locking the many-side while pinning the other table's key must fire: %+v", ms)
	}
}

func TestLockingNonUniqueOrderFiresByDefault(t *testing.T) {
	// [R6] ORDER BY status is not a total order over the locked set.
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	sql := "SELECT * FROM t WHERE status = 'x' ORDER BY status FOR UPDATE;\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 1 {
		t.Fatalf("non-unique ORDER BY must fire under the default: %+v", ms)
	}
}

func TestLockingUniqueOrderPasses(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	sql := "SELECT * FROM t WHERE status = 'x' ORDER BY created_at, id FOR UPDATE;\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 0 {
		t.Fatalf("ORDER BY including id must pass: %+v", ms)
	}
}

func TestLockingNonUniqueOrderPassesWhenRelaxed(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\norder_requires_unique_key: false\n")
	sql := "SELECT * FROM t WHERE status = 'x' ORDER BY status FOR UPDATE;\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 0 {
		t.Fatalf("with order_requires_unique_key:false any ORDER BY passes: %+v", ms)
	}
}

// Fix 1: lockingSelects misses CTEs feeding UPDATE/DELETE.

func TestLockingInCTEFeedingUpdateSelected(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	sql := "WITH locked AS (SELECT id FROM t WHERE status='x' FOR UPDATE) UPDATE t SET status='y' WHERE id IN (SELECT id FROM locked);\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 1 {
		t.Fatalf("locking SELECT in a CTE feeding an UPDATE must fire: %+v", ms)
	}
}

func TestLockingInCTEFeedingDeleteSelected(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	sql := "WITH locked AS (SELECT id FROM t WHERE status='x' FOR UPDATE) DELETE FROM t WHERE id IN (SELECT id FROM locked);\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 1 {
		t.Fatalf("locking SELECT in a CTE feeding a DELETE must fire: %+v", ms)
	}
}

// Fix 2: ORDER BY total-order check must be relation-aware.

func TestLockingOrderByOtherRelationKeyFires(t *testing.T) {
	// FOR UPDATE OF oi locks order_items, but ORDER BY o.id sorts by the OTHER
	// relation's key — not a total order over the locked (oi) rows.
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	sql := "SELECT * FROM orders o JOIN order_items oi ON oi.order_id=o.id WHERE o.status='x' ORDER BY o.id FOR UPDATE OF oi;\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 1 {
		t.Fatalf("ORDER BY the wrong relation's key must fire: %+v", ms)
	}
}

func TestLockingOrderByLockedRelationKeyPasses(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	sql := "SELECT * FROM orders o JOIN order_items oi ON oi.order_id=o.id WHERE o.status='x' ORDER BY oi.id FOR UPDATE OF oi;\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 0 {
		t.Fatalf("ORDER BY the locked relation's own key must pass: %+v", ms)
	}
}

// Fix 3: key=column must not be exempted as a single-row lookup.

func TestLockingKeyEqualsColumnFires(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	sql := "SELECT * FROM t WHERE id = owner_id FOR UPDATE;\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 1 {
		t.Fatalf("id = owner_id (column, not param/const) must fire: %+v", ms)
	}
}

func TestLockingKeyEqualsParamStillPasses(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	sql := "SELECT * FROM t WHERE id = $1 FOR UPDATE;\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 0 {
		t.Fatalf("id = $1 must still pass: %+v", ms)
	}
}

func TestLockingSingleRowCastParamPasses(t *testing.T) {
	// $1::uuid is a cast-wrapped param — still a single-row PK lookup, must pass.
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	sql := "SELECT * FROM t WHERE id = $1::uuid FOR UPDATE;\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 0 {
		t.Fatalf("cast-wrapped param single-row lock must be exempt: %+v", ms)
	}
}

func TestLockingParamEqualsKeyStillPasses(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	sql := "SELECT * FROM t WHERE $1 = id FOR UPDATE;\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 0 {
		t.Fatalf("$1 = id must still pass: %+v", ms)
	}
}

// Fix 4: unique_key_columns must fold case like PostgreSQL identifier folding.

func TestLockingUpperCaseConfiguredKeyStillExempts(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [ID]\n")
	sql := "SELECT * FROM t WHERE id = $1 FOR UPDATE;\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 0 {
		t.Fatalf("unique_key_columns:[ID] must still exempt a parsed lowercase id: %+v", ms)
	}
}

func TestLockingMixedCaseConfiguredKeyStillExempts(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [UserId]\n")
	sql := "SELECT * FROM t WHERE UserId = $1 FOR UPDATE;\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 0 {
		t.Fatalf("unique_key_columns:[UserId] must still exempt a folded userid: %+v", ms)
	}
}

// Fix 5: ORDER BY ordinal must be resolved against the target list.

func TestLockingOrderByOrdinalOfKeyPasses(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	sql := "SELECT id, status FROM t WHERE status='x' ORDER BY 1 FOR UPDATE;\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 0 {
		t.Fatalf("ORDER BY 1 resolving to the unique key must pass: %+v", ms)
	}
}

func TestLockingOrderByOrdinalOfNonKeyFires(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	sql := "SELECT id, status FROM t WHERE status='x' ORDER BY 2 FOR UPDATE;\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 1 {
		t.Fatalf("ORDER BY 2 resolving to a non-key column must fire: %+v", ms)
	}
}

// Fix #12: a locking query built by string concatenation or fmt.Sprintf over
// a runtime variable is reassembled (placeholder-substituted) so this rule
// can see it, rather than being invisible because it never folds to a
// compile-time-constant literal.

func TestLockingConcatenatedQueryFires(t *testing.T) {
	src := "package db\n\nfunc q(tbl string) string {\n\treturn \"SELECT * FROM \" + tbl + \" WHERE status='x' FOR UPDATE\"\n}\n"
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	if ms := matches(t, c, file("q.go", src)); len(ms) != 1 {
		t.Fatalf("concatenated locking query with no ORDER BY must fire: %+v", ms)
	}
}

func TestLockingSprintfQueryFires(t *testing.T) {
	src := "package db\n\nimport \"fmt\"\n\nfunc q(t string) string {\n\treturn fmt.Sprintf(\"SELECT * FROM %s WHERE status='x' FOR UPDATE\", t)\n}\n"
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	if ms := matches(t, c, file("q.go", src)); len(ms) != 1 {
		t.Fatalf("Sprintf-composed locking query with no ORDER BY must fire: %+v", ms)
	}
}

func TestLockingGoFullyLiteralOrderedQueryPasses(t *testing.T) {
	// No regression: a fully-literal .go locking query with a deterministic
	// ORDER BY must still pass.
	src := "package db\n\nfunc q() string {\n\treturn \"SELECT * FROM t WHERE status='x' ORDER BY id FOR UPDATE\"\n}\n"
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	if ms := matches(t, c, file("q.go", src)); len(ms) != 0 {
		t.Fatalf("fully-literal ordered .go locking query must pass: %+v", ms)
	}
}

// A dynamic ORDER BY column reassembles to `ORDER BY fw_expr`, which cannot
// be verified as a total order, so the strict default flags it (conservative).
func TestLockingDynamicOrderByColumnFiresByDefault(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	src := "package db\n\nfunc q(col string) string { return \"SELECT * FROM t WHERE status='x' ORDER BY \" + col + \" FOR UPDATE\" }\n"
	if ms := matches(t, c, file("db.go", src)); len(ms) != 1 {
		t.Fatalf("dynamic ORDER BY column must fire under the strict default: %+v", ms)
	}
}

// Fix #11: a cheap pre-parse gate skips WASM-parsing entirely for a file
// whose content contains neither "update" nor "share" (case-insensitive) —
// no locking clause (FOR UPDATE, FOR NO KEY UPDATE, FOR SHARE, FOR KEY
// SHARE) can exist without one of those words, so this is safe.

func TestLockingPureDDLFileNoFindings(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	sql := "CREATE TABLE t (id int);\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 0 {
		t.Fatalf("a pure-DDL file with no update/share word must yield no findings: %+v", ms)
	}
}

// Fix: a bare FOR UPDATE over a join locks EVERY base relation, but that must
// not make the ORDER BY arm unreachable. An ORDER BY that pins a unique key of
// every locked relation is a total order over the locked set and is compliant;
// one that pins only some of them is not.

func TestLockingJoinOrderedByAllLockedKeysPasses(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	sql := "SELECT o.*, c.name FROM orders o JOIN customers c ON c.id = o.customer_id ORDER BY o.id, c.id FOR UPDATE;\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 0 {
		t.Fatalf("ORDER BY every locked relation's key is a total order and must pass: %+v", ms)
	}
}

func TestLockingJoinOrderedByOneLockedKeyFires(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	sql := "SELECT o.*, c.name FROM orders o JOIN customers c ON c.id = o.customer_id ORDER BY o.id FOR UPDATE;\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 1 {
		t.Fatalf("ORDER BY only one of two locked relations' keys is not a total order: %+v", ms)
	}
}

func TestLockingJoinOrderedByAllLockedKeysPassesWhenRelaxed(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\norder_requires_unique_key: false\n")
	sql := "SELECT o.*, c.name FROM orders o JOIN customers c ON c.id = o.customer_id ORDER BY o.name FOR UPDATE;\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 0 {
		t.Fatalf("with order_requires_unique_key:false any ORDER BY passes over a join too: %+v", ms)
	}
}

// Fix: the non-column side of a unique-key equality only has to be
// non-varying-per-row. A function call or a scalar subquery pins one value
// just as a bind parameter does; requiring literally ParamRef/A_Const turns
// these single-row lookups into violations.

func TestLockingKeyEqualsFuncCallPasses(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	sql := "SELECT * FROM t WHERE id = decode_id($1) FOR UPDATE;\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 0 {
		t.Fatalf("id = f($1) is still a single-row pin: %+v", ms)
	}
}

func TestLockingKeyEqualsCastFuncCallPasses(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	sql := "SELECT * FROM t WHERE id = current_setting('app.tenant')::uuid FOR UPDATE;\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 0 {
		t.Fatalf("id = f(...)::uuid is still a single-row pin: %+v", ms)
	}
}

func TestLockingKeyEqualsFuncCallOfColumnFires(t *testing.T) {
	// The function pins nothing: its argument is a column, so the right-hand
	// value varies per row and the lock is not single-row.
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	sql := "SELECT * FROM t WHERE id = coalesce(owner_id, 0) FOR UPDATE;\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 1 {
		t.Fatalf("id = f(column) varies per row and must fire: %+v", ms)
	}
}

func TestLockingKeyEqualsScalarSubqueryPasses(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	sql := "SELECT * FROM t WHERE id = (SELECT account_id FROM session WHERE token = $1) FOR UPDATE;\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 0 {
		t.Fatalf("id = (scalar subquery) is still a single-row pin: %+v", ms)
	}
}

func TestLockingKeyEqualsInSubqueryStillFires(t *testing.T) {
	// IN (...) is AEXPR_IN, not a plain equality — many rows, must still fire.
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	sql := "SELECT * FROM t WHERE id IN (SELECT account_id FROM session) FOR UPDATE;\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 1 {
		t.Fatalf("id IN (subquery) is multi-row and must fire: %+v", ms)
	}
}

// Fix: ORDER BY an output-column alias must resolve through the target list,
// exactly as ORDER BY an ordinal already does.

func TestLockingOrderByOutputAliasOfKeyPasses(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	sql := "SELECT id AS pk, status FROM jobs WHERE status='x' ORDER BY pk FOR UPDATE;\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 0 {
		t.Fatalf("ORDER BY an alias of the unique key must pass: %+v", ms)
	}
}

func TestLockingOrderByOutputAliasOfNonKeyFires(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	sql := "SELECT status AS s, id FROM jobs WHERE status='x' ORDER BY s FOR UPDATE;\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 1 {
		t.Fatalf("ORDER BY an alias of a non-key column must fire: %+v", ms)
	}
}

func TestLockingOrderByQualifiedNameIsNotAliasResolved(t *testing.T) {
	// A qualified sort item names a relation column, never an output alias —
	// alias resolution must not rescue the wrong relation's key.
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\n")
	sql := "SELECT oi.id AS id FROM orders o JOIN order_items oi ON oi.order_id=o.id WHERE o.status='x' ORDER BY o.id FOR UPDATE OF oi;\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 1 {
		t.Fatalf("a qualified sort item must not be alias-resolved: %+v", ms)
	}
}

func TestLockingDynamicOrderByColumnPassesWhenRelaxed(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "unique_key_columns: [id]\norder_requires_unique_key: false\n")
	src := "package db\n\nfunc q(col string) string { return \"SELECT * FROM t WHERE status='x' ORDER BY \" + col + \" FOR UPDATE\" }\n"
	if ms := matches(t, c, file("db.go", src)); len(ms) != 0 {
		t.Fatalf("with order_requires_unique_key:false any ORDER BY passes: %+v", ms)
	}
}
