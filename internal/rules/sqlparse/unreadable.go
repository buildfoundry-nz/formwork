package sqlparse

import (
	"errors"
	"fmt"
	"strings"

	"github.com/buildfoundry-nz/formwork/internal/sqlextract"
)

// unreadable.go — #311. WHICH EXTRACTOR A RULE ACTUALLY SOURCES THROUGH.
//
// The #75 census exists so an operator can ask "how many queries in this repo
// did the gate decline to analyse" and get a number instead of a doc comment.
// It asked sqlextract.FromGo for every rule whose type starts with "sql/", and
// only two of the four registered types source through FromGo. Both locking
// types go through lockingStatements → sqlextract.FromGoReassembled, which
// RESOLVES fmt.Sprint{,f,ln} and one-sided '+' chains into fw_expr placeholder
// text and analyses them — so FromGo's answer, attributed to a locking rule, is
// wrong in both directions at once:
//
//   - every line it printed denied analysis of a composition the rule reads. One
//     repo, one run: `formwork check` failing on db/q.go:6 and `formwork lint`
//     calling that same line "not analysed by this rule".
//   - and none of the fold's real limits produced a Site at all, so a repo whose
//     files each hide an unordered locking SELECT behind a strings.Builder, a
//     loop, a called closure and `lockIt(&q)` reported a clean run and an empty
//     census. The channel reported precisely what the rule DOES read and was
//     silent about everything it does not — the inverse of the invariant #75
//     cites.
//
// So the source dispatch lives here, next to the rules whose sourcing it
// describes, rather than in the census as a prefix match on a type name.

// ErrExtractorUnknown is what CensusSites answers for a sql/* rule type that
// neither reassembledTypes nor fromGoTypes names.
//
// A CENSUS LINE, NOT A SKIP, is what a caller owes this error. It is not a
// parse failure — there may be nothing wrong with the file at all — it is the
// census saying it does not know which extractor the rule sourced through and
// therefore has nothing true to say about what the rule declined to read.
// Swallowing it reports that rule's coverage as clean, which is the silence
// #311 half 2 is about; printing FromGo's answer instead is the false claim
// half 1 is about.
var ErrExtractorUnknown = errors.New("formwork: the census does not know which extractor this rule type sources through")

// UnreadableSites returns the SQL-bearing compositions in a .go file that the
// locking rules' extractor declined to read.
//
// FILTERED BY looksLikeSQL, on the site's seed text. The fold tracks every
// string-literal-seeded local, most of which hold paths, messages and format
// strings; reporting each one an operator's code untracks would bury the SQL
// gate's real coverage gaps under the whole repo. It is the same filter
// statements() applies on the FromGo path (parses.go), for the same reason and
// with the same residual: a fragment with a leading SQL keyword and no other
// structural keyword is not SQL-shaped and is not reported.
//
// A file that is not .go has no Go compositions and returns nothing. err is a
// Go AST parse failure only, which is the rule's to report, not the census's.
func UnreadableSites(path string, content []byte) ([]sqlextract.Site, error) {
	if sqlextract.FileKind(path) != "go" {
		return nil, nil
	}
	_, sites, err := sqlextract.FromGoReassembled(path, content)
	if err != nil {
		return nil, err
	}
	var out []sqlextract.Site
	for _, s := range sites {
		if looksLikeSQL(s.Text) {
			out = append(out, s)
		}
	}
	return out, nil
}

// reassembledTypes are the rule types that source through
// sqlextract.FromGoReassembled, and therefore carry the assignment-flow fold's
// coverage limits rather than FromGo's. Both are in this package; sql/parses
// (parses.go) and sqltext's sql/statement-predicate are the FromGo half.
//
// A LIST, NOT A PATTERN, and that is the correction: #75 read the mapping off
// the type NAME, so every future sql/* type inherited FromGo's answer whether
// it sources through FromGo or not. A type registered without a line here gets
// the FromGo answer too — but it gets it from a table a reader can check
// against the rule's own CheckFile, and
// TestEverySQLRuleTypeNamesTheExtractorItSourcesThrough fails on a registered
// type this file does not account for.
var reassembledTypes = map[string]bool{
	"sql/locking-select-order": true,
	"sql/locking-target":       true,
}

// fromGoTypes are the sql/* types that really do source through
// sqlextract.FromGo: sql/parses (source.go's statements) and sqltext's
// sql/statement-predicate.
//
// THE DEFAULT IS A REFUSAL, NOT THIS SET, which is what makes the table load
// bearing in the binary and not only under `go test`. Leaving FromGo as the
// fall-through meant a type registered without a line here got FromGo's answer
// printed under its own id — the guess #311 was filed about, arriving by the
// route #311 describes, a mapping read off the type NAME. A type in neither
// table now gets ErrExtractorUnknown.
var fromGoTypes = map[string]bool{
	"sql/parses":              true,
	"sql/statement-predicate": true,
}

// CensusSites returns the compositions the rule TYPE ruleType declines to read
// in path, for the #75 escape-hatch census.
//
// ONE FUNCTION, BECAUSE THE MAPPING FROM RULE TYPE TO EXTRACTOR IS THE THING
// #311 IS ABOUT. A caller that picks an extractor itself has re-derived that
// mapping, which is how the census came to attribute FromGo's limits to a rule
// that never calls it. ok is false for a type that is not a SQL rule at all, so
// a caller cannot silently get an answer meant for one.
//
// The FromGo half is unfiltered, deliberately: FromGo emits a Site only for a
// composition it already identified as string-valued (unresolvedReason), so
// there is no flood to filter, and filtering would newly hide compositions the
// census has reported since #75.
//
// A sql/* type in NEITHER table is refused with ErrExtractorUnknown rather than
// sourced by whichever extractor happens to be written last here. ok stays true
// — it is a SQL rule, and the caller asked the right question — so the refusal
// reaches the caller as the census's own gap, which it owes a line, and not as
// "not a SQL rule", which it owes nothing.
func CensusSites(ruleType, path string, content []byte) ([]sqlextract.Site, bool, error) {
	if !strings.HasPrefix(ruleType, "sql/") {
		return nil, false, nil
	}
	if reassembledTypes[ruleType] {
		sites, err := UnreadableSites(path, content)
		return sites, true, err
	}
	if !fromGoTypes[ruleType] {
		return nil, true, fmt.Errorf("%w: %s", ErrExtractorUnknown, ruleType)
	}
	if sqlextract.FileKind(path) != "go" {
		return nil, true, nil
	}
	_, sites, err := sqlextract.FromGo(path, content)
	return sites, true, err
}

// AccountedForByTheCensus reports whether ruleType is one whose extractor
// CensusSites knows rather than guesses. Exported for the test that reads the
// live registry, which is the only thing that can say when a new type has
// appeared — and CensusSites now enforces the same property at runtime, so a
// type this returns false for is refused with ErrExtractorUnknown rather than
// handed FromGo's answer.
func AccountedForByTheCensus(ruleType string) bool {
	return reassembledTypes[ruleType] || fromGoTypes[ruleType]
}
