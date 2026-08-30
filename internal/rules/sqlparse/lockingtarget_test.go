package sqlparse_test

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/rules"
)

// newChecker is checker's error-returning twin: config refusals are part of this
// rule's contract, so they need asserting rather than fataling.
func newChecker(t *testing.T, typ, params string) (rules.Checker, error) {
	t.Helper()
	factory, ok := rules.Lookup(typ)
	if !ok {
		t.Fatalf("type %q not registered", typ)
	}
	return factory(paramsNode(t, params))
}

// #37 — sql/locking-target answers the question sql/locking-select-order cannot:
// WHICH relation is locked, and how strongly.
//
// The two are different hazards. Ordering prevents two writers taking the same
// rows in different sequences. An exclusive lock on a SPECIFIC row is dangerous
// regardless of ordering, when another writer holds a child row and wants that
// row at a weaker strength — the FK FOR KEY SHARE cycle. A single-row lock is
// just as deadly there, so the ORDER BY rule correctly passes it and the hazard
// goes unpoliced.
//
// ALIAS RESOLUTION IS THE POINT. A regex cannot bind `p` to `app.projects`, so
// the downstream replacement for this rule had to choose between four false
// positives and losing one true positive — and chose to lose it. Every test here
// exists because a naive token check gets it wrong.

func TestLockingTargetResolvesAliasToTheGuardedTable(t *testing.T) {
	c := checker(t, "sql/locking-target", "table: 'projects'\nstrength: [update]\n")
	sql := "SELECT * FROM app.projects AS p JOIN app.pages pg ON pg.project_id = p.id FOR UPDATE OF p;\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 1 {
		t.Fatalf("FOR UPDATE OF p where p binds the guarded table must fire: %+v", ms)
	}
}

// The four-out-of-five case the regex got right, and this must too: an alias
// bound to a DIFFERENT table is not the guarded row.
func TestLockingTargetIgnoresAnAliasBoundElsewhere(t *testing.T) {
	c := checker(t, "sql/locking-target", "table: 'projects'\nstrength: [update]\n")
	sql := "SELECT * FROM app.projects AS p JOIN app.pages pg ON pg.project_id = p.id FOR UPDATE OF pg;\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 0 {
		t.Fatalf("FOR UPDATE OF pg locks pages, not the guarded table: %+v", ms)
	}
}

// A bare FOR UPDATE with no OF locks every base relation in the FROM clause,
// including the guarded one.
func TestLockingTargetBareForUpdateLocksEveryBaseRelation(t *testing.T) {
	c := checker(t, "sql/locking-target", "table: 'projects'\nstrength: [update]\n")
	sql := "SELECT * FROM app.projects AS p JOIN app.pages pg ON pg.project_id = p.id FOR UPDATE;\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 1 {
		t.Fatalf("a bare FOR UPDATE locks the guarded table too: %+v", ms)
	}
}

// Strength is the other half. The FK cycle this rule exists for is about
// EXCLUSIVE locks; a KEY SHARE on the same row is the compatible side and must
// not fire when only `update` is guarded.
func TestLockingTargetIgnoresAWeakerStrength(t *testing.T) {
	c := checker(t, "sql/locking-target", "table: 'projects'\nstrength: [update]\n")
	sql := "SELECT * FROM app.projects AS p FOR KEY SHARE OF p;\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 0 {
		t.Fatalf("FOR KEY SHARE is not an exclusive lock: %+v", ms)
	}
}

func TestLockingTargetMatchesADeclaredWeakStrength(t *testing.T) {
	c := checker(t, "sql/locking-target", "table: 'projects'\nstrength: [key-share]\n")
	sql := "SELECT * FROM app.projects AS p FOR KEY SHARE OF p;\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 1 {
		t.Fatalf("a declared strength must fire: %+v", ms)
	}
}

// An unaliased relation is identified by its own name, and the guarded table
// may be schema-qualified in the source.
func TestLockingTargetMatchesAnUnaliasedRelation(t *testing.T) {
	c := checker(t, "sql/locking-target", "table: 'projects'\nstrength: [update]\n")
	sql := "SELECT * FROM app.projects FOR UPDATE OF projects;\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 1 {
		t.Fatalf("an unaliased relation is identified by its relname: %+v", ms)
	}
}

// A non-locking SELECT is out of scope entirely.
func TestLockingTargetIgnoresANonLockingSelect(t *testing.T) {
	c := checker(t, "sql/locking-target", "table: 'projects'\nstrength: [update]\n")
	sql := "SELECT * FROM app.projects AS p;\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 0 {
		t.Fatalf("no locking clause, nothing to report: %+v", ms)
	}
}

// Config errors are exit 2, never a rule that silently matches nothing.
func TestLockingTargetRejectsAnUnknownStrength(t *testing.T) {
	if _, err := newChecker(t, "sql/locking-target", "table: 'projects'\nstrength: [exclusive]\n"); err == nil {
		t.Fatal("an unknown strength must be refused at config time")
	}
}

func TestLockingTargetRequiresATable(t *testing.T) {
	if _, err := newChecker(t, "sql/locking-target", "strength: [update]\n"); err == nil {
		t.Fatal("table is required")
	}
}

// #294 / #37 — SCHEMA QUALIFICATION.
//
// `other.projects` and `public.projects` are different tables that happen to
// share a relname, and #37's acceptance criteria require the rule to respect
// the difference. It did not: relationBindings stored the relname alone,
// GetSchemaname() was read nowhere in the repo, and the operator's obvious
// remedy — `table: 'public.projects'` — matched nothing while `formwork check`
// reported OK at exit 0. Both halves are pinned below, in both directions.

func TestLockingTargetRespectsSchemaQualification(t *testing.T) {
	c := checker(t, "sql/locking-target", "table: 'projects'\nschema: 'public'\nstrength: [update]\n")
	sql := "SELECT o.id FROM other.projects o ORDER BY o.id FOR UPDATE OF o;\n" +
		"SELECT o.id FROM public.projects o ORDER BY o.id FOR UPDATE OF o;\n"
	ms := matches(t, c, file("q.sql", sql))
	if len(ms) != 1 {
		t.Fatalf("a lock on a same-named table in an unguarded schema is not the guarded row; want 1 finding, got %d: %+v", len(ms), ms)
	}
	if ms[0].Line != 2 {
		t.Fatalf("the finding must be public.projects (line 2), not other.projects (line 1): %+v", ms[0])
	}
}

// The fail-closed half of the same decision. An unqualified relation carries NO
// schema in the parse tree — PostgreSQL resolves it through search_path at
// execution time, which this rule cannot read. Reporting it is the only sound
// answer for a deadlock guard: the guarded table is the likeliest thing a bare
// `projects` resolves to, and a guard that goes quiet on the ambiguous case
// misses the hazard it exists for.
func TestLockingTargetFiresOnAnUnqualifiedRelationWhenASchemaIsGuarded(t *testing.T) {
	c := checker(t, "sql/locking-target", "table: 'projects'\nschema: 'public'\nstrength: [update]\n")
	sql := "SELECT * FROM projects FOR UPDATE OF projects;\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 1 {
		t.Fatalf("an unqualified relation's schema is decided by search_path, not by the text, so the guard must not assume it away: %+v", ms)
	}
}

// The default must not narrow. An absent `schema` guards every schema, which is
// what TestLockingTargetMatchesAnUnaliasedRelation already relies on — pinned
// here on the discrimination corpus so a future default cannot quietly shrink
// the guard to one schema.
func TestLockingTargetWithoutASchemaGuardsEverySchema(t *testing.T) {
	c := checker(t, "sql/locking-target", "table: 'projects'\nstrength: [update]\n")
	sql := "SELECT o.id FROM other.projects o ORDER BY o.id FOR UPDATE OF o;\n" +
		"SELECT o.id FROM public.projects o ORDER BY o.id FOR UPDATE OF o;\n"
	if ms := matches(t, c, file("q.sql", sql)); len(ms) != 2 {
		t.Fatalf("an unconfigured schema guards every schema: %+v", ms)
	}
}

// Two findings reading `guarded lock on projects` are indistinguishable, and one
// of them may be the wrong table. The finding says which.
func TestLockingTargetNamesTheSchemaInItsFinding(t *testing.T) {
	c := checker(t, "sql/locking-target", "table: 'projects'\nstrength: [update]\n")
	sql := "SELECT o.id FROM other.projects o ORDER BY o.id FOR UPDATE OF o;\n"
	ms := matches(t, c, file("q.sql", sql))
	if len(ms) != 1 {
		t.Fatalf("want 1 finding, got %d: %+v", len(ms), ms)
	}
	if !strings.Contains(ms[0].Message, "other.projects") {
		t.Fatalf("a finding on a schema-qualified relation must name the schema, or a false positive is unreadable: %q", ms[0].Message)
	}
}

// Config errors are exit 2, never a rule that silently matches nothing.
// `table` is matched against the relation NAME alone, so a schema-qualified
// value can never fire — including the natural `<schema>.<table>` spelling
// anyone who has written SQL reaches for, and the one the cases below use.
func TestLockingTargetRefusesASchemaQualifiedTable(t *testing.T) {
	for _, tbl := range []string{"public.projects", `public\.projects`} {
		_, err := newChecker(t, "sql/locking-target", "table: '"+tbl+"'\nstrength: [update]\n")
		if err == nil {
			t.Fatalf("table: %q is matched against the relname alone and can never fire; it must be refused, not accepted as a rule that matches nothing", tbl)
		}
		// A refusal that does not spell the working config is a dead end. Both
		// spellings of the dot — bare and regex-escaped — must be split the
		// same way, or the escaped one suggests a schema of `public\`.
		for _, want := range []string{`params.schema: "public"`, `params.table: "projects"`} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("the refusal of table: %q must spell the config that works (%s); got: %v", tbl, want, err)
			}
		}
	}
}

// ...and the refusal must not swallow a legitimate pattern over the relname.
func TestLockingTargetAcceptsARelnamePattern(t *testing.T) {
	for _, tbl := range []string{"^projects$", "proj.*", "projects|pages", ".*"} {
		if _, err := newChecker(t, "sql/locking-target", "table: '"+tbl+"'\nstrength: [update]\n"); err != nil {
			t.Fatalf("table: %q is a pattern over the relation name, not a schema qualification: %v", tbl, err)
		}
	}
}

func TestLockingTargetRefusesAnInvalidSchemaRegex(t *testing.T) {
	_, err := newChecker(t, "sql/locking-target", "table: 'projects'\nschema: '('\nstrength: [update]\n")
	if err == nil {
		t.Fatal("an unparseable schema regex is a config error, exit 2")
	}
	if !strings.Contains(err.Error(), "params.schema") {
		t.Fatalf("the refusal must name the parameter at fault: %v", err)
	}
}
