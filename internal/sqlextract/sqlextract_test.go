package sqlextract_test

import (
	"strings"
	"testing"

	"github.com/buildfoundry-nz/formwork/internal/sqlextract"
)

func TestFromGoSingleLiteral(t *testing.T) {
	src := "package db\n\nfunc q() string { return \"SELECT * FROM orders;\" }\n"
	got, unresolved, err := sqlextract.FromGo("db.go", []byte(src))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(got) != 1 || got[0].Text != "SELECT * FROM orders;" {
		t.Fatalf("want one candidate with the literal, got %+v", got)
	}
	if got[0].Line != 3 {
		t.Fatalf("want candidate line 3, got %d", got[0].Line)
	}
	if len(unresolved) != 0 {
		t.Fatalf("a plain literal is not unresolved: %+v", unresolved)
	}
}

func TestFromGoConcatFolds(t *testing.T) {
	src := "package db\n\nconst c = \"CREATE TABLE users (\" +\n\t\"id \" +\n\t\"int)\"\n"
	got, _, err := sqlextract.FromGo("db.go", []byte(src))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(got) != 1 || got[0].Text != "CREATE TABLE users (id int)" {
		t.Fatalf("want one folded candidate, got %+v", got)
	}
}

func TestFromGoSprintfIsUnresolved(t *testing.T) {
	// [R: acceptance (b)] a fmt.Sprintf-composed query must surface as an
	// unresolved Site, not be silently dropped.
	src := "package db\n\nimport \"fmt\"\n\nfunc q(t string) string {\n\treturn fmt.Sprintf(\"SELECT * FROM %s\", t)\n}\n"
	_, unresolved, err := sqlextract.FromGo("db.go", []byte(src))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(unresolved) != 1 {
		t.Fatalf("want one unresolved site for the Sprintf query, got %+v", unresolved)
	}
	if unresolved[0].Line != 6 {
		t.Fatalf("want unresolved site on line 6, got %d", unresolved[0].Line)
	}
}

func TestFromGoMixedConcatIsUnresolved(t *testing.T) {
	src := "package db\n\nfunc q(t string) string { return \"SELECT * FROM \" + t }\n"
	_, unresolved, err := sqlextract.FromGo("db.go", []byte(src))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(unresolved) != 1 {
		t.Fatalf("want one unresolved site for the mixed concat, got %+v", unresolved)
	}
}

func TestFromGoParseFailureIsError(t *testing.T) {
	if _, _, err := sqlextract.FromGo("bad.go", []byte("package db\n\nfunc broken( {\n")); err == nil {
		t.Fatal("a .go file that fails to parse must return an error")
	}
}

// [R9] fragments of unresolvable queries are still collected (sqltext needs
// them, byte-identically) but flagged Partial so the parse-tree rules can skip
// them and not false-positive.
func TestFromGoSprintfFragmentIsPartial(t *testing.T) {
	src := "package db\n\nimport \"fmt\"\n\nfunc q(t string) string { return fmt.Sprintf(\"SELECT * FROM %s\", t) }\n"
	resolved, unresolved, err := sqlextract.FromGo("db.go", []byte(src))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(unresolved) != 1 {
		t.Fatalf("want 1 unresolved site, got %+v", unresolved)
	}
	var sawPartial, found bool
	for _, c := range resolved {
		if c.Text == "SELECT * FROM %s" {
			found, sawPartial = true, c.Partial
		}
	}
	if !found {
		t.Fatalf("the Sprintf format-string candidate must still be collected: %+v", resolved)
	}
	if !sawPartial {
		t.Fatalf("the Sprintf format-string candidate must be marked Partial: %+v", resolved)
	}
}

func TestFromGoMixedConcatFragmentIsPartial(t *testing.T) {
	src := "package db\n\nfunc q(t string) string { return \"SELECT * FROM \" + t }\n"
	resolved, _, err := sqlextract.FromGo("db.go", []byte(src))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	found := false
	for _, c := range resolved {
		if c.Text == "SELECT * FROM " {
			found = true
			if !c.Partial {
				t.Fatalf("concat fragment must be Partial: %+v", c)
			}
		}
	}
	if !found {
		t.Fatalf("the concat fragment candidate must still be collected: %+v", resolved)
	}
}

func TestFromGoCompleteLiteralNotPartial(t *testing.T) {
	src := "package db\n\nfunc q() string { return \"SELECT * FROM orders;\" }\n"
	resolved, _, err := sqlextract.FromGo("db.go", []byte(src))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(resolved) != 1 || resolved[0].Partial {
		t.Fatalf("a complete literal must not be Partial: %+v", resolved)
	}
}

// --- FromGoReassembled ---

func TestFromGoReassembledLoneLiteralReturnsItself(t *testing.T) {
	src := "package db\n\nfunc q() string { return \"SELECT * FROM orders;\" }\n"
	got, _, err := sqlextract.FromGoReassembled("db.go", []byte(src))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(got) != 1 || got[0].Text != "SELECT * FROM orders;" {
		t.Fatalf("want one candidate with the literal itself, got %+v", got)
	}
	if got[0].Partial {
		t.Fatalf("a reassembled candidate must never be Partial: %+v", got[0])
	}
}

func TestFromGoReassembledConcatSubstitutesPlaceholder(t *testing.T) {
	src := "package db\n\nfunc q(tbl string) string {\n\treturn \"SELECT * FROM \" + tbl + \" WHERE status='x' FOR UPDATE\"\n}\n"
	got, _, err := sqlextract.FromGoReassembled("db.go", []byte(src))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want exactly one reassembled candidate, got %+v", got)
	}
	want := "SELECT * FROM fw_expr WHERE status='x' FOR UPDATE"
	if got[0].Text != want {
		t.Fatalf("want %q, got %q", want, got[0].Text)
	}
	if got[0].Partial {
		t.Fatalf("a reassembled candidate must never be Partial: %+v", got[0])
	}
}

func TestFromGoReassembledSprintfSubstitutesPlaceholder(t *testing.T) {
	// The source also has an import path literal ("fmt"), which sqlextract
	// does not SQL-shape-filter (same as FromGo) — that gating is a caller
	// concern (parses.go's looksLikeSQL, locking's parse-or-skip). So this
	// asserts the wanted candidate is present, not that it is the only one.
	src := "package db\n\nimport \"fmt\"\n\nfunc q(t string, n int) string {\n\treturn fmt.Sprintf(\"SELECT * FROM %s WHERE id=%d\", t, n)\n}\n"
	got, _, err := sqlextract.FromGoReassembled("db.go", []byte(src))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	want := "SELECT * FROM fw_expr WHERE id=fw_expr"
	found := false
	for _, c := range got {
		if c.Text == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("want a candidate %q, got %+v", want, got)
	}
}

func TestFromGoReassembledStringsBuilderPairIsTwoCandidates(t *testing.T) {
	// A strings.Builder composition cannot be reassembled at all; each
	// WriteString argument surfaces as its own lone-literal candidate, not a
	// merged one. (The source also has an import path literal ("strings"),
	// which sqlextract does not SQL-shape-filter — same as FromGo.)
	src := "package db\n\nimport \"strings\"\n\nfunc q() string {\n\tvar b strings.Builder\n\tb.WriteString(\"SELECT id, \")\n\tb.WriteString(\"name FROM t\")\n\treturn b.String()\n}\n"
	got, _, err := sqlextract.FromGoReassembled("db.go", []byte(src))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	var foundFirst, foundSecond bool
	for _, c := range got {
		if c.Text == "SELECT id, " {
			foundFirst = true
		}
		if c.Text == "name FROM t" {
			foundSecond = true
		}
	}
	if !foundFirst || !foundSecond {
		t.Fatalf("want the two literals as separate candidates, got %+v", got)
	}
}

func TestFromGoReassembledVerbForms(t *testing.T) {
	src := "package db\n\nimport \"fmt\"\n\nfunc q(a, b, c string) string { return fmt.Sprintf(\"SELECT %[1]s, %+v FROM t WHERE p = %.2f AND q = 100%% ok\", a, b, c) }\n"
	cands, _, err := sqlextract.FromGoReassembled("db.go", []byte(src))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	// every verb -> fw_expr; %% -> literal %
	want := "SELECT fw_expr, fw_expr FROM t WHERE p = fw_expr AND q = 100% ok"
	found := false
	for _, cd := range cands {
		if cd.Text == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("verb/%%%% substitution wrong; want %q in %+v", want, cands)
	}
}

// Fix: fmt.Sprint/Sprintln concatenate ALL their operands — treating args[0]
// as a format string and discarding args[1:] silently drops trailing SQL text
// (including a real ORDER BY), which can turn a compliant locking query into a
// false finding.

func TestFromGoReassembledSprintKeepsTrailingOperands(t *testing.T) {
	src := "package db\n\nimport \"fmt\"\n\nfunc q(sfx string) string {\n\treturn fmt.Sprint(\"SELECT * FROM jobs WHERE t = $1 FOR UPDATE\", sfx)\n}\n"
	got, _, err := sqlextract.FromGoReassembled("db.go", []byte(src))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	want := "SELECT * FROM jobs WHERE t = $1 FOR UPDATE" + "fw_expr"
	found := false
	for _, c := range got {
		if c.Text == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("fmt.Sprint must contribute every operand; want %q in %+v", want, got)
	}
}

func TestFromGoReassembledSprintDoesNotSubstituteVerbs(t *testing.T) {
	// args[0] of fmt.Sprint is NOT a format string: a literal "%s" in it is
	// literal text, not a verb.
	src := "package db\n\nimport \"fmt\"\n\nfunc q() string { return fmt.Sprint(\"SELECT '100%s' FROM t\") }\n"
	got, _, err := sqlextract.FromGoReassembled("db.go", []byte(src))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	want := "SELECT '100%s' FROM t"
	found := false
	for _, c := range got {
		if c.Text == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("fmt.Sprint must not verb-substitute; want %q in %+v", want, got)
	}
}

func TestFromGoReassembledSprintlnSeparatesOperands(t *testing.T) {
	src := "package db\n\nimport \"fmt\"\n\nfunc q(sfx string) string {\n\treturn fmt.Sprintln(\"SELECT * FROM t FOR UPDATE\", sfx)\n}\n"
	got, _, err := sqlextract.FromGoReassembled("db.go", []byte(src))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	want := "SELECT * FROM t FOR UPDATE fw_expr\n"
	found := false
	for _, c := range got {
		if c.Text == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("fmt.Sprintln must space-separate operands and append a newline; want %q in %+v", want, got)
	}
}

// Fix: a '+' operand that is itself a reassemblable composition (a Sprintf
// call) must contribute its own text, not a bare placeholder — otherwise the
// SQL inside it is lost entirely, because FromGoReassembled stops descending
// once the '+' expression is consumed.

func TestFromGoReassembledConcatWithSprintfOperandKeepsSQL(t *testing.T) {
	src := "package db\n\nimport \"fmt\"\n\nfunc q(p, tbl string) string {\n\treturn p + fmt.Sprintf(\"SELECT id FROM %s WHERE ready FOR UPDATE\", tbl)\n}\n"
	got, _, err := sqlextract.FromGoReassembled("db.go", []byte(src))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	want := "fw_exprSELECT id FROM fw_expr WHERE ready FOR UPDATE"
	found := false
	for _, c := range got {
		if c.Text == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("a Sprintf operand of a '+'-chain must contribute its SQL; want %q in %+v", want, got)
	}
}

// Fix: a '+' expression with no string-literal part anywhere is not a string
// composition at all (e.g. integer arithmetic). Claiming it as a candidate
// both manufactures a junk "fw_exprfw_expr" text and — worse — stops the walk
// descending into operands that may themselves hold SQL.

func TestFromGoReassembledIntegerAdditionIsNotACandidate(t *testing.T) {
	src := "package db\n\nfunc f(a, b int) int { return a + b }\n"
	got, _, err := sqlextract.FromGoReassembled("db.go", []byte(src))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	for _, c := range got {
		if c.Text == "fw_exprfw_expr" {
			t.Fatalf("a literal-free '+' expression must not become a candidate: %+v", got)
		}
	}
}

func TestFromGoReassembledDescendsIntoLiteralFreeAddition(t *testing.T) {
	// Neither operand of the outer '+' is a string literal, so the expression
	// itself is not a candidate — but the Sprintf inside one operand still is,
	// and must be reached.
	src := "package db\n\nimport \"fmt\"\n\nfunc q(p string, n int) string {\n\treturn p + fmt.Sprintf(\"SELECT * FROM t WHERE n = %d FOR UPDATE\", n)\n}\n"
	got, _, err := sqlextract.FromGoReassembled("db.go", []byte(src))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	found := false
	for _, c := range got {
		if strings.Contains(c.Text, "SELECT * FROM t WHERE n = fw_expr FOR UPDATE") {
			found = true
		}
	}
	if !found {
		t.Fatalf("SQL nested in a '+' operand must still be reached: %+v", got)
	}
}

func TestFromGoReassembledParseFailureIsError(t *testing.T) {
	if _, _, err := sqlextract.FromGoReassembled("bad.go", []byte("package db\n\nfunc broken( {\n")); err == nil {
		t.Fatal("a .go file that fails to parse must return an error")
	}
}

// Fix #13: FileKind is the single shared .sql/.go extension-dispatch helper
// used by both sqlparse and sqltext, replacing duplicated inline switches.

func TestFileKindSQL(t *testing.T) {
	for _, p := range []string{"q.sql", "Q.SQL", "dir/mixed.Sql"} {
		if got := sqlextract.FileKind(p); got != "sql" {
			t.Fatalf("FileKind(%q) = %q, want \"sql\"", p, got)
		}
	}
}

func TestFileKindGo(t *testing.T) {
	for _, p := range []string{"db.go", "DB.GO", "dir/mixed.Go"} {
		if got := sqlextract.FileKind(p); got != "go" {
			t.Fatalf("FileKind(%q) = %q, want \"go\"", p, got)
		}
	}
}

func TestFileKindOther(t *testing.T) {
	for _, p := range []string{"README.md", "noext", "q.sql.bak", "q.txt"} {
		if got := sqlextract.FileKind(p); got != "" {
			t.Fatalf("FileKind(%q) = %q, want \"\"", p, got)
		}
	}
}
