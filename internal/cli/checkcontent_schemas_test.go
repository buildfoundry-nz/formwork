package cli_test

import "testing"

// A locking-select-order rule that reads params.schemas judges a .sql file
// against schema files beside it, so a single-content check defers it rather
// than erroring for want of a tree, and the rest of the file is still judged.
func TestCheckContentDefersLockingOrderThatReadsSchemas(t *testing.T) {
	rules := `rules:
  - id: sql-parses
    type: sql/parses
    scope: {include: ['**']}
    params: {}
  - id: sql-lock-order
    type: sql/locking-select-order
    scope: {include: ['**']}
    params: {schemas: [db/schema.sql]}
`
	root := contentRepo(t, "", rules)
	code, doc, errOut := runContent(t, "SELECT id FROM app.t WHERE id = $1 FOR UPDATE;\n", "check-content", "-C", root, "--path", "db/q.sql")
	if code != 0 || doc.Coverage.Evaluated != 1 || doc.Coverage.Deferred != 1 {
		t.Fatalf("a schema-reading lock-order rule must defer on .sql: code=%d doc=%+v stderr=%s", code, doc, errOut)
	}
	if reason := notEvaluatedReasons(doc)["sql-lock-order"]; reason != "content_only_not_supported" {
		t.Errorf("sql-lock-order reason = %q, want content_only_not_supported", reason)
	}
}
