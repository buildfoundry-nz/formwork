package sqlextract

import (
	"go/ast"
	"regexp"
	"strings"
)

// explain_wrapper.go — EXPLAIN AROUND A STATEMENT ADDS NO STATEMENT.
//
// A plan gate or a plan tool wraps a statement it did not write: "EXPLAIN (…) " + q.
// Nothing about that wrapper can name a table or change what q does, and q itself is
// judged where it is built: a literal or constant is a candidate there, a composition
// is a site there, and a parameter is its caller's composition. So the wrapper is not
// a composition of its own to read.
//
// ONLY THE WRAPPER. The literal is EXPLAIN and its options and nothing else, and the
// one hole is the whole rest of the statement. A statement started in the literal, a
// second statement, an option decided at run time, text after the hole, or a second
// hole all stay sites.
//
// DISCLOSED LIMIT. Text that enters the program from outside, an operator's query or a
// file, has no origin in the tree to be judged at. EXPLAIN without ANALYZE executes
// nothing. EXPLAIN ANALYZE executes its statement, so a tool passing outside text to
// it must guard that text itself.
var explainWrapper = regexp.MustCompile(`(?i)^\s*EXPLAIN(\s+ANALY[SZ]E|\s+VERBOSE)*(\s*\([^()]*\))?\s+$`)

// isExplainWrapperFormat reports whether a fmt.Sprintf is the wrapper around its one
// %s argument: fmt.Sprintf("EXPLAIN (…) %s", q).
func isExplainWrapperFormat(fn, format string, args []ast.Expr) bool {
	return fn == "Sprintf" && len(args) == 2 && strings.HasSuffix(format, "%s") &&
		explainWrapper.MatchString(strings.TrimSuffix(format, "%s"))
}
