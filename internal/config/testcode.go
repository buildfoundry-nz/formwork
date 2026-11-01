// testcode.go — the one definition of test code (formwork.yaml `tests:`) and
// a rule's statement that its subject is production code (scope.subject).
// Split from config.go, which the 750-line vendor cap bounds.
//
// WHY A SUBJECT AND NOT AN EXCLUDE. Every rule judges tests by default: a test
// is code, and a rule about code reads it. Some rules are about something only
// production code can do — a production import of a testing package, a
// reflection call in shipped code, a request message no production handler
// consumes — and for those a test is not a weaker instance of the subject; it
// is not the subject at all, or (for a consumer census) it is evidence the
// rule must not count. Before 0.15 each such rule said so with its own list of
// test globs in scope.exclude: one copy per rule, indistinguishable from an
// exclude that forgives a violation, and wrong the day a test convention
// moved.
//
// A repository's test code is a fact about its toolchains (`*_test.go` is a
// Go test because `go build` leaves it out), so it is declared ONCE, here. A
// rule whose subject is production code says `scope.subject: production`;
// every other rule reads tests. `formwork lint` lists each production-subject
// rule with the number of test files its subject leaves out, and refuses one
// whose subject leaves out nothing, so the statement is never a silent
// subtraction.
package config

import (
	"fmt"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// TestCode is the repository's definition of test code: path globs, matched
// like scope.include. A nil *TestCode declares none.
type TestCode struct {
	Globs []string
}

func compileTests(globs []string) (*TestCode, error) {
	if len(globs) == 0 {
		return nil, nil
	}
	t := &TestCode{}
	seen := map[string]bool{}
	for i, g := range globs {
		where := fmt.Sprintf("tests[%d]", i)
		if strings.TrimSpace(g) == "" {
			return nil, fmt.Errorf("%s: glob is empty", where)
		}
		if !doublestar.ValidatePattern(g) {
			return nil, fmt.Errorf("%s: invalid glob %q", where, g)
		}
		if seen[g] {
			return nil, fmt.Errorf("%s: glob %q is declared twice", where, g)
		}
		seen[g] = true
		t.Globs = append(t.Globs, g)
	}
	return t, nil
}

// Contains reports whether relPath (slash-separated) is test code.
func (t *TestCode) Contains(relPath string) bool {
	return t != nil && matchStored(t.Globs, relPath)
}

// Subject values for scope.subject.
const (
	SubjectAll        = "all"
	SubjectProduction = "production"
)

func parseSubject(s string) (production bool, err error) {
	switch s {
	case "", SubjectAll:
		return false, nil
	case SubjectProduction:
		return true, nil
	}
	return false, fmt.Errorf("unknown subject %q (want %q or %q)", s, SubjectAll, SubjectProduction)
}

// SubjectIsProduction reports whether the rule declares scope.subject:
// production, so it reads no file the repository's `tests:` names.
func (r *Rule) SubjectIsProduction() bool { return r.productionOnly }

// TestCode returns the repository's definition of test code, shared by every
// rule loaded from it; nil for a rule built with New.
func (r *Rule) TestCode() *TestCode { return r.testCode }
