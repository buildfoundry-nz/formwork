package sqlparse

import "testing"

func TestParseSplitsMultipleStatements(t *testing.T) {
	res, err := parse("SELECT 1; SELECT 2;")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got := len(res.GetStmts()); got != 2 {
		t.Fatalf("want 2 statements, got %d", got)
	}
}

func TestParseDoesNotSplitSemicolonInStringLiteral(t *testing.T) {
	// [R1] the naive ';' splitter this replaces would mis-split this into two
	// broken fragments; the real parser keeps it as ONE valid statement.
	res, err := parse("INSERT INTO x(msg) VALUES('hello; world');")
	if err != nil {
		t.Fatalf("valid Postgres must parse clean, got: %v", err)
	}
	if got := len(res.GetStmts()); got != 1 {
		t.Fatalf("want 1 statement, got %d", got)
	}
}

func TestParseSyntaxErrorReturnsError(t *testing.T) {
	if _, err := parse("SELECT FROM;"); err == nil {
		t.Fatal("a syntax error must return an error")
	}
}

func TestLineAt(t *testing.T) {
	content := "a\nb\nc"
	for off, want := range map[int]int{0: 1, 2: 2, 4: 3} {
		if got := lineAt(content, off); got != want {
			t.Fatalf("lineAt(%d)=%d, want %d", off, got, want)
		}
	}
}

func TestParseConcurrent(t *testing.T) {
	const n = 32
	done := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			_, err := parse("SELECT * FROM t WHERE id = $1 FOR UPDATE;")
			done <- err
		}()
	}
	for i := 0; i < n; i++ {
		if err := <-done; err != nil {
			t.Fatalf("concurrent parse err: %v", err)
		}
	}
}
