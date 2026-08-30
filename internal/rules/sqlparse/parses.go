package sqlparse

import (
	"fmt"
	"strings"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"gopkg.in/yaml.v3"
)

// sqlStartKeywords are the statement-initial SQL keywords that make a Go string
// literal "SQL-shaped". Deliberately conservative — the SQL-ness gate for the
// .go source path (a heuristic), so sql/parses does not fire on import paths,
// struct tags, or log strings.
// Deliberately excluded, though all are real statement-initial SQL keywords:
// SET, SHOW, COMMENT, ANALYZE, TABLE, VALUES. Each is also an everyday English
// word, and combined with the common-English entries in
// sqlStructuralKeywords (ON, AS, ORDER, SET) they make ordinary prose literals
// — "Set the order to pending", "Comment on the record as needed" — look like
// SQL and get reported as unparseable. The statements they introduce are rare
// in Go string literals; the prose is not. Not worth the trade.
var sqlStartKeywords = map[string]bool{
	"SELECT": true, "INSERT": true, "UPDATE": true, "DELETE": true,
	"WITH": true, "CREATE": true, "ALTER": true, "DROP": true,
	"TRUNCATE": true, "GRANT": true, "REVOKE": true, "EXPLAIN": true,
}

// sqlDDLLeadKeywords are the leading keywords whose statements are recognised
// by the object they act on rather than by a later structural keyword — see
// looksLikeSQL's DDL arm.
var sqlDDLLeadKeywords = map[string]bool{
	"CREATE": true, "ALTER": true, "DROP": true, "TRUNCATE": true,
}

// sqlDDLObjectKeywords name the kind of object a DDL statement acts on. One of
// these adjacent to a sqlDDLLeadKeywords token (CREATE TABLE, DROP INDEX) is a
// SQL signal as strong as a structural keyword, and short DDL has no
// structural keyword to offer.
var sqlDDLObjectKeywords = map[string]bool{
	"TABLE": true, "INDEX": true, "VIEW": true, "SEQUENCE": true,
	"SCHEMA": true, "DATABASE": true, "TYPE": true, "DOMAIN": true,
	"FUNCTION": true, "PROCEDURE": true, "TRIGGER": true, "RULE": true,
	"EXTENSION": true, "ROLE": true, "USER": true, "POLICY": true,
	"TABLESPACE": true, "AGGREGATE": true, "OPERATOR": true,
	"COLLATION": true, "CAST": true, "SERVER": true,
	"PUBLICATION": true, "SUBSCRIPTION": true, "STATISTICS": true,
}

// sqlDDLModifiers may appear between a DDL leading keyword and its object
// keyword (CREATE UNIQUE INDEX, CREATE OR REPLACE VIEW, CREATE MATERIALIZED
// VIEW, DROP TABLE IF EXISTS). They are skipped when looking for the object.
var sqlDDLModifiers = map[string]bool{
	"OR": true, "REPLACE": true, "UNIQUE": true, "MATERIALIZED": true,
	"TEMP": true, "TEMPORARY": true, "UNLOGGED": true, "GLOBAL": true,
	"LOCAL": true, "RECURSIVE": true, "CONCURRENTLY": true, "FOREIGN": true,
	"IF": true, "NOT": true, "EXISTS": true, "ONLY": true,
}

// sqlStructuralKeywords are the SQL keywords that, found as a whole word
// anywhere after the leading token, corroborate that a leading-keyword match
// is actual SQL rather than prose or a bare fragment. Real SQL of any
// substance has at least one of these beyond its first token; ordinary
// sentences that merely start with a SQL-shaped word ("SELECT a plan to
// continue") and lone fragments built up via strings.Builder ("SELECT id, ")
// do not.
var sqlStructuralKeywords = map[string]bool{
	"FROM": true, "WHERE": true, "JOIN": true, "INTO": true,
	"VALUES": true, "SET": true, "RETURNING": true, "USING": true,
	"GROUP": true, "ORDER": true, "HAVING": true, "LIMIT": true,
	"ON": true, "AS": true,
}

type parses struct{}

func newParses(node *yaml.Node) (rules.Checker, error) {
	var p struct{}
	if err := rules.DecodeParams(node, &p); err != nil {
		return nil, err
	}
	return &parses{}, nil
}

// CheckFile flags each SQL statement that fails to parse. For .sql files every
// failure is reported; for .go files only SQL-shaped candidates are, so
// non-SQL string literals never fire.
func (c *parses) CheckFile(f *scan.File) ([]rules.Match, error) {
	_, fails, err := statements(f)
	if err != nil {
		return nil, err
	}
	var ms []rules.Match
	for _, fail := range fails {
		if fail.FromGo && !looksLikeSQL(fail.Text) {
			continue
		}
		ms = append(ms, rules.Match{
			Line:    fail.Line,
			Message: fmt.Sprintf("SQL does not parse: %v", parseErrMsg(fail.Err)),
		})
	}
	return ms, nil
}

// looksLikeSQL reports whether text is SQL-shaped enough for a .go parse
// failure to be worth reporting. It requires structural completeness, not
// just a leading keyword: after stripping leading whitespace, comments, and
// any leading '(' (the start of a parenthesized SELECT), the first token
// must be a statement-initial SQL keyword (sqlStartKeywords), AND the text
// must corroborate that by one of two arms:
//
//   - the DDL arm: the leading keyword is CREATE/ALTER/DROP/TRUNCATE and the
//     next word — skipping DDL modifiers like UNIQUE or OR REPLACE — names a
//     DDL object (TABLE, INDEX, VIEW, …). Short DDL ("CREATE TABLE users (…)")
//     carries no structural keyword at all, so without this arm it was
//     silently dropped from .go coverage entirely;
//   - the structural arm: the text contains at least one SQL structural
//     keyword (sqlStructuralKeywords) as a whole word beyond the first token.
//
// The DDL arm requires ADJACENCY (modulo modifiers) precisely so it does not
// become a second prose loophole: "Drop the file quietly" has "the" where the
// object keyword would be, and is not matched.
//
// Residual false negative, accepted: a DDL form with neither an object keyword
// nor a structural keyword — "TRUNCATE t" — is not flagged. Crediting a bare
// leading TRUNCATE would readmit "Truncate the log file" as SQL.
//
// This is deliberately still a heuristic and stays leaky by design: it can
// still false-fire on prose that happens to contain a second structural word
// (e.g. "SELECT items FROM your cart below"). That residual is accepted —
// eliminating it would need real SQL tokenization of Go string literals,
// which sql/parses does not do. The point of the second-keyword requirement
// is only to filter out the common case of a lone leading keyword with no
// SQL structure at all, which covers ordinary prose and single-fragment
// strings.Builder chunks.
//
// sqlStructuralKeywords includes common English words (ON, AS, SET, ORDER),
// so the false-positive surface described above is broader than the single
// example suggests: any leading-keyword sentence that also happens to contain
// one of these words as a whole word — e.g. a sentence with "...in order
// to..." — can also false-fire. This is accepted for the same reason: these
// words are legitimate SQL structural keywords, and excluding them would
// narrow real-SQL coverage for no principled gain.
//
// The structural arm also has a symmetric residual false negative, by design:
// a fragment with a leading keyword and no other structural keyword in the
// literal (e.g. "SELECT id, " built up through a strings.Builder) is NOT
// flagged. Fragments like these are only caught when they also contain a
// structural keyword, or when assembled into a complete, still-malformed
// statement.
func looksLikeSQL(text string) bool {
	s := stripLeading(text)
	for strings.HasPrefix(s, "(") {
		s = stripLeading(s[1:])
	}
	i := 0
	for i < len(s) && isAlpha(s[i]) {
		i++
	}
	if i == 0 {
		return false
	}
	lead := strings.ToUpper(s[:i])
	if !sqlStartKeywords[lead] {
		return false
	}
	if sqlDDLLeadKeywords[lead] && hasAdjacentDDLObject(s[i:]) {
		return true
	}
	return hasStructuralKeyword(s[i:])
}

// hasAdjacentDDLObject reports whether the first word of s — skipping any run
// of DDL modifiers — names a DDL object. s is the remainder of a literal after
// its leading DDL keyword.
func hasAdjacentDDLObject(s string) bool {
	for {
		word, rest, ok := nextWord(s)
		if !ok {
			return false
		}
		up := strings.ToUpper(word)
		if sqlDDLObjectKeywords[up] {
			return true
		}
		if !sqlDDLModifiers[up] {
			return false
		}
		s = rest
	}
}

// nextWord returns the first whole word in s (letters, digits, underscore) and
// the remainder after it. ok is false when s holds no word.
func nextWord(s string) (word, rest string, ok bool) {
	i := 0
	for i < len(s) && !isWordChar(s[i]) {
		i++
	}
	if i == len(s) {
		return "", "", false
	}
	j := i
	for j < len(s) && isWordChar(s[j]) {
		j++
	}
	return s[i:j], s[j:], true
}

// hasStructuralKeyword reports whether s contains any sqlStructuralKeywords
// entry as a whole word (case-insensitive). Word characters are letters,
// digits, and underscore, so e.g. "FROM2" or "my_from" do not match "FROM".
func hasStructuralKeyword(s string) bool {
	start := -1
	check := func(end int) bool {
		if start < 0 {
			return false
		}
		word := s[start:end]
		start = -1
		return sqlStructuralKeywords[strings.ToUpper(word)]
	}
	for i := 0; i < len(s); i++ {
		if isWordChar(s[i]) {
			if start < 0 {
				start = i
			}
			continue
		}
		if check(i) {
			return true
		}
	}
	return check(len(s))
}

func isWordChar(b byte) bool { return isAlpha(b) || (b >= '0' && b <= '9') || b == '_' }

func stripLeading(s string) string {
	for {
		s = strings.TrimLeft(s, " \t\r\n")
		switch {
		case strings.HasPrefix(s, "--"):
			if nl := strings.IndexByte(s, '\n'); nl >= 0 {
				s = s[nl+1:]
				continue
			}
			return ""
		case strings.HasPrefix(s, "/*"):
			if end := strings.Index(s, "*/"); end >= 0 {
				s = s[end+2:]
				continue
			}
			return ""
		default:
			return s
		}
	}
}

func isAlpha(b byte) bool { return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') }

func parseErrMsg(err error) string {
	if err == nil {
		return "unknown error"
	}
	return err.Error()
}

func init() {
	rules.Register("sql/parses", newParses)
}
