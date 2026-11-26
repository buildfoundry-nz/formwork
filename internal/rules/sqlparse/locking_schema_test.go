package sqlparse_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
)

// The schema every case below reads: a composite primary key, a single-column
// primary key on a column not named id, a UNIQUE constraint, a partial unique
// index and an expression index.
const lockingSchemaSQL = `
CREATE TABLE app.rows (session_id uuid NOT NULL, row_key integer NOT NULL, note text);
ALTER TABLE ONLY app.rows ADD CONSTRAINT rows_pkey PRIMARY KEY (session_id, row_key);
CREATE TABLE app.overrides (item_id uuid NOT NULL, price numeric);
ALTER TABLE ONLY app.overrides ADD CONSTRAINT overrides_pkey PRIMARY KEY (item_id);
CREATE TABLE app.profiles (id uuid PRIMARY KEY, org_id uuid NOT NULL, site_id uuid, logo text);
CREATE UNIQUE INDEX profiles_org_only ON app.profiles USING btree (org_id) WHERE (site_id IS NULL);
CREATE TABLE app.carts (id uuid PRIMARY KEY, project_id uuid NOT NULL);
CREATE TABLE app.projects (id uuid PRIMARY KEY, current_cart_id uuid, UNIQUE (current_cart_id));
CREATE TABLE app.tags (id uuid PRIMARY KEY, label text NOT NULL);
CREATE UNIQUE INDEX tags_label_lower ON app.tags USING btree (lower(label));
CREATE TABLE app.labels (id uuid PRIMARY KEY, org_id uuid NOT NULL, label text NOT NULL);
CREATE UNIQUE INDEX labels_org_label ON app.labels USING btree (org_id, lower(label));
CREATE TABLE app.groups (id uuid PRIMARY KEY, project_id uuid NOT NULL, ordinal integer NOT NULL, colour text);
ALTER TABLE ONLY app.groups ADD CONSTRAINT groups_project_ordinal UNIQUE (project_id, ordinal);
CREATE TABLE app.rules (id uuid PRIMARY KEY, org_id uuid NOT NULL, site_id uuid);
CREATE UNIQUE INDEX rules_org_site ON app.rules USING btree (org_id, COALESCE(site_id, '00000000-0000-0000-0000-000000000000'::uuid));
CREATE TABLE app.scored (id uuid PRIMARY KEY, body jsonb);
CREATE UNIQUE INDEX scored_body ON app.scored USING btree (((body ->> 'k')));
CREATE TABLE app.slots (id uuid PRIMARY KEY, slot integer, n integer NOT NULL);
CREATE UNIQUE INDEX slots_slot ON app.slots USING btree (COALESCE(slot, (n + 1)));
`

// schemaChecker builds the rule with params.schema and returns it with a
// function that judges one query file walked beside the schema.
func schemaChecker(t *testing.T) (rules.Checker, func(sql string) []rules.Match) {
	t.Helper()
	c := checker(t, "sql/locking-select-order", "schemas: [db/schema.sql]\n")
	return c, func(sql string) []rules.Match {
		t.Helper()
		f := walkedFile(t, map[string]string{"db/schema.sql": lockingSchemaSQL, "q.sql": sql}, "q.sql")
		return matches(t, c, f)
	}
}

func walkedFile(t *testing.T, files map[string]string, want string) *scan.File {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	fs, err := scan.Walk(root)
	if err != nil {
		t.Fatal(err)
	}
	f := fs.File(want)
	if f == nil {
		t.Fatalf("walk kept no %s", want)
	}
	return f
}

func TestSchemaKeysCompositeOrderPasses(t *testing.T) {
	_, judge := schemaChecker(t)
	if ms := judge("SELECT 1 FROM app.rows c WHERE c.note = 'x' ORDER BY c.session_id, c.row_key FOR UPDATE OF c;\n"); len(ms) != 0 {
		t.Fatalf("ORDER BY the whole composite key is a total order: %+v", ms)
	}
}

func TestSchemaKeysPartOfCompositeOrderFires(t *testing.T) {
	_, judge := schemaChecker(t)
	if ms := judge("SELECT 1 FROM app.rows c WHERE c.note = 'x' ORDER BY c.row_key FOR UPDATE OF c;\n"); len(ms) != 1 {
		t.Fatalf("ORDER BY one column of a composite key is not a total order: %+v", ms)
	}
}

func TestSchemaKeysNonIDPrimaryKeyOrderPasses(t *testing.T) {
	_, judge := schemaChecker(t)
	if ms := judge("SELECT item_id FROM app.overrides WHERE item_id = ANY($1) ORDER BY item_id FOR UPDATE;\n"); len(ms) != 0 {
		t.Fatalf("ORDER BY a primary key named item_id is a total order: %+v", ms)
	}
}

func TestSchemaKeysNonKeyOrderFires(t *testing.T) {
	_, judge := schemaChecker(t)
	if ms := judge("SELECT 1 FROM app.overrides WHERE price > 0 ORDER BY price FOR UPDATE;\n"); len(ms) != 1 {
		t.Fatalf("ORDER BY a non-key column is not a total order: %+v", ms)
	}
}

func TestSchemaKeysCompositeLookupPasses(t *testing.T) {
	_, judge := schemaChecker(t)
	if ms := judge("SELECT note FROM app.rows WHERE session_id = $1 AND row_key = $2 FOR UPDATE;\n"); len(ms) != 0 {
		t.Fatalf("pinning every column of the key is a single-row lookup: %+v", ms)
	}
}

func TestSchemaKeysHalfCompositeLookupFires(t *testing.T) {
	_, judge := schemaChecker(t)
	if ms := judge("SELECT note FROM app.rows WHERE session_id = $1 FOR UPDATE;\n"); len(ms) != 1 {
		t.Fatalf("pinning half a composite key locks many rows: %+v", ms)
	}
}

func TestSchemaKeysPartialIndexWithItsPredicatePasses(t *testing.T) {
	_, judge := schemaChecker(t)
	if ms := judge("SELECT logo FROM app.profiles WHERE org_id = $1 AND site_id IS NULL FOR UPDATE;\n"); len(ms) != 0 {
		t.Fatalf("a partial unique key whose predicate the query repeats pins one row: %+v", ms)
	}
}

func TestSchemaKeysPartialIndexWithoutItsPredicateFires(t *testing.T) {
	_, judge := schemaChecker(t)
	if ms := judge("SELECT logo FROM app.profiles WHERE org_id = $1 FOR UPDATE;\n"); len(ms) != 1 {
		t.Fatalf("org_id is unique only where site_id IS NULL: %+v", ms)
	}
}

func TestSchemaKeysExpressionOfAPinnedColumnPasses(t *testing.T) {
	_, judge := schemaChecker(t)
	if ms := judge("SELECT id FROM app.tags WHERE label = $1 FOR UPDATE;\n"); len(ms) != 0 {
		t.Fatalf("label = $1 fixes lower(label), which the unique index makes one row: %+v", ms)
	}
}

func TestSchemaKeysExpressionOfAnUnpinnedColumnFires(t *testing.T) {
	_, judge := schemaChecker(t)
	if ms := judge("SELECT id FROM app.tags WHERE id <> $1 FOR UPDATE;\n"); len(ms) != 1 {
		t.Fatalf("nothing pins label, so lower(label) is no single value: %+v", ms)
	}
}

func TestSchemaKeysCoalesceKeyPinnedByIsNullPasses(t *testing.T) {
	_, judge := schemaChecker(t)
	if ms := judge("SELECT id FROM app.rules WHERE org_id = $1 AND site_id IS NULL FOR UPDATE;\n"); len(ms) != 0 {
		t.Fatalf("org_id = $1 AND site_id IS NULL pins (org_id, COALESCE(site_id, …)): %+v", ms)
	}
}

func TestSchemaKeysCoalesceKeyWithoutSiteFires(t *testing.T) {
	_, judge := schemaChecker(t)
	if ms := judge("SELECT id FROM app.rules WHERE org_id = $1 FOR UPDATE;\n"); len(ms) != 1 {
		t.Fatalf("org_id alone leaves every site's row: %+v", ms)
	}
}

func TestSchemaKeysIsNotNullPinsNothing(t *testing.T) {
	_, judge := schemaChecker(t)
	if ms := judge("SELECT id FROM app.rules WHERE org_id = $1 AND site_id IS NOT NULL FOR UPDATE;\n"); len(ms) != 1 {
		t.Fatalf("site_id IS NOT NULL leaves every site's row: %+v", ms)
	}
}

func TestSchemaKeysExpressionWithAnUnreadablePartIsNoKey(t *testing.T) {
	_, judge := schemaChecker(t)
	if ms := judge("SELECT id FROM app.slots WHERE slot = $1 FOR UPDATE;\n"); len(ms) != 1 {
		t.Fatalf("COALESCE(slot, n + 1) holds an operator this does not read, so it is no key: %+v", ms)
	}
}

func TestSchemaKeysUnsupportedExpressionIsNoKey(t *testing.T) {
	_, judge := schemaChecker(t)
	if ms := judge("SELECT id FROM app.scored WHERE body = $1 FOR UPDATE;\n"); len(ms) != 1 {
		t.Fatalf("an index on body ->> 'k' is not read as a key: %+v", ms)
	}
}

func TestSchemaKeysPinnedColumnCoversTheOrder(t *testing.T) {
	_, judge := schemaChecker(t)
	if ms := judge("SELECT colour FROM app.groups WHERE project_id = $1 ORDER BY ordinal FOR UPDATE;\n"); len(ms) != 0 {
		t.Fatalf("one project's groups ordered by ordinal are in (project_id, ordinal) order: %+v", ms)
	}
}

func TestSchemaKeysOrderMissingTheUnpinnedColumnFires(t *testing.T) {
	_, judge := schemaChecker(t)
	if ms := judge("SELECT colour FROM app.groups WHERE colour = 'red' ORDER BY ordinal FOR UPDATE;\n"); len(ms) != 1 {
		t.Fatalf("ordinal alone repeats across projects: %+v", ms)
	}
}

func TestSchemaKeysReadFromEverySchemaListed(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "schemas: [db/a.sql, db/b.sql]\n")
	f := walkedFile(t, map[string]string{
		"db/a.sql": "CREATE TABLE app.x (id uuid PRIMARY KEY);\n",
		"db/b.sql": "CREATE TABLE ext.part_config (parent_table text PRIMARY KEY, n int);\n",
		"q.sql":    "SELECT 1 FROM ext.part_config pc WHERE n > 0 ORDER BY pc.parent_table FOR UPDATE OF pc;\n",
	}, "q.sql")
	if ms := matches(t, c, f); len(ms) != 0 {
		t.Fatalf("a key from the second schema file counts: %+v", ms)
	}
}

func TestSchemaKeysJoinPinnedRelationPasses(t *testing.T) {
	_, judge := schemaChecker(t)
	sql := "SELECT b.id FROM app.carts b JOIN app.projects p ON p.current_cart_id = b.id WHERE p.id = $1 FOR UPDATE OF b;\n"
	if ms := judge(sql); len(ms) != 0 {
		t.Fatalf("a cart joined on its key to one project row is one row: %+v", ms)
	}
}

func TestSchemaKeysJoinPinnedEitherWayRound(t *testing.T) {
	_, judge := schemaChecker(t)
	sql := "SELECT b.id FROM app.carts b JOIN app.projects p ON b.id = p.current_cart_id WHERE p.id = $1 FOR UPDATE OF b;\n"
	if ms := judge(sql); len(ms) != 0 {
		t.Fatalf("the join equality pins b.id whichever side it is written on: %+v", ms)
	}
}

func TestSchemaKeysIndexWithAnExpressionColumnIsNoKey(t *testing.T) {
	_, judge := schemaChecker(t)
	if ms := judge("SELECT id FROM app.labels WHERE org_id = $1 FOR UPDATE;\n"); len(ms) != 1 {
		t.Fatalf("a unique index on (org_id, lower(label)) does not make org_id a key: %+v", ms)
	}
}

func TestSchemaKeysJoinOnNonKeyFires(t *testing.T) {
	_, judge := schemaChecker(t)
	sql := "SELECT b.id FROM app.carts b JOIN app.projects p ON p.id = b.project_id WHERE p.id = $1 FOR UPDATE OF b;\n"
	if ms := judge(sql); len(ms) != 1 {
		t.Fatalf("every cart of one project is many rows: %+v", ms)
	}
}

func TestSchemaKeysOuterJoinQualPinsNothing(t *testing.T) {
	_, judge := schemaChecker(t)
	sql := "SELECT b.id FROM app.projects p LEFT JOIN app.carts b ON p.current_cart_id = b.id WHERE p.id = $1 FOR UPDATE OF b;\n"
	if ms := judge(sql); len(ms) != 1 {
		t.Fatalf("an outer join's qual does not restrict its null-extended side: %+v", ms)
	}
}

func TestSchemaKeysOrPinsNothing(t *testing.T) {
	_, judge := schemaChecker(t)
	if ms := judge("SELECT 1 FROM app.overrides WHERE item_id = $1 OR price > 0 FOR UPDATE;\n"); len(ms) != 1 {
		t.Fatalf("an OR at the top pins no row: %+v", ms)
	}
}

func TestSchemaKeysBothLockedRelationsSingleRowPass(t *testing.T) {
	_, judge := schemaChecker(t)
	sql := "SELECT b.id FROM app.carts b JOIN app.projects p ON p.current_cart_id = b.id WHERE p.id = $1 FOR UPDATE;\n"
	if ms := judge(sql); len(ms) != 0 {
		t.Fatalf("two locked relations, each one row, have no siblings to order: %+v", ms)
	}
}

func TestSchemaFileMissingIsAnError(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "schemas: [db/absent.sql]\n")
	f := walkedFile(t, map[string]string{"q.sql": "SELECT 1 FROM app.rows WHERE note = 'x' FOR UPDATE;\n"}, "q.sql")
	_, err := c.CheckFile(f)
	if err == nil || !strings.Contains(err.Error(), "db/absent.sql") {
		t.Fatalf("a schema the tree does not hold must be an error naming it, got %v", err)
	}
}

func TestSchemaFileThatDoesNotParseIsAnError(t *testing.T) {
	c := checker(t, "sql/locking-select-order", "schemas: [db/schema.sql]\n")
	f := walkedFile(t, map[string]string{"db/schema.sql": "CREATE TABLE (;\n", "q.sql": "SELECT 1 FROM t WHERE a = 'x' FOR UPDATE;\n"}, "q.sql")
	if _, err := c.CheckFile(f); err == nil {
		t.Fatal("a schema that does not parse must be an error, never an empty key set")
	}
}

// A rule that reads params.schemas judges a file against other files, so it is
// never content-only: a single-content check would have no tree to read the
// schemas from. Without schemas, a .sql file stays content-only.
func TestLockingOrderWithSchemasIsNotContentOnly(t *testing.T) {
	c, _ := schemaChecker(t)
	if rules.IsContentOnly(c, "q.sql") {
		t.Fatal("sql/locking-select-order with schemas claims a content-only verdict for q.sql")
	}
	plain := checker(t, "sql/locking-select-order", "")
	if !rules.IsContentOnly(plain, "q.sql") {
		t.Fatal("sql/locking-select-order without schemas lost its content-only verdict for q.sql")
	}
	if rules.IsContentOnly(plain, "q.go") {
		t.Fatal("sql/locking-select-order claims a content-only verdict for a .go file")
	}
}
