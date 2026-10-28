package sqlparse

import (
	"strings"

	"github.com/buildfoundry-nz/formwork/internal/sqlextract"
)

// sqlbearing.go — WHICH UNREADABLE COMPOSITIONS ARE SQL.
//
// sqlextract reports every string composition it cannot reassemble: a '+'
// chain with one non-constant side, a fmt.Sprint{,f,ln} call. Most of those in
// a real repository are not SQL at all. One measured corpus had 22,693 of them
// in the files its 71 SQL rules govern, and 2,040 carried SQL; the rest were
// error messages, file paths, regular expressions, e-mail bodies and shell
// commands. A census that lists all 22,693 as "SQL … could not be read" states
// something false about 20,653 lines, and a rule that failed closed on that set
// would fail on every log message in scope.
//
// sqlBearing is the test, applied to the composition's reassembled text, where
// each operand the extractor could not read stands as the placeholder. A
// composition is SQL when its literal text carries any of:
//
//   - a statement: looksLikeSQL, the test sql/parses applies to a literal,
//     once the placeholder is a word of its own — "SELECT" + cols otherwise
//     reads as the one word "SELECTfw_expr"; or a statement keyword in
//     capitals followed by more of the statement (opensWithStatement), which
//     reads "SELECT pg_advisory_lock(" + key + ")" and "RESET ROLE".
//   - the same after an operand, at the start of a line, or after a ';': a
//     shared CTE or column list composed in front of a statement the literal
//     spells in full. After an operand looksLikeSQL counts only in capitals,
//     since a template's prose continues an operand with "with … as" as
//     readily as SQL does.
//   - a clause in those places or opening the text: WHERE, ORDER BY, LIMIT,
//     JOIN and the like, in capitals or beside a SQL token. AND, OR and NOT
//     count only beside a SQL token, whatever their case: a Cloud Logging
//     filter and a shouted sentence both use them.
//   - a SQL token anywhere (hasSQLToken): a cast, a positional parameter in
//     parameter position, IS [NOT] NULL, IS [NOT] DISTINCT FROM, = ANY(,
//     COALESCE(, NULLIF(, a parenthesised SELECT.
//
// RESIDUALS, both directions, measured on that corpus. Not reported: SQL in
// lower case with no SQL token ("where (" + col + " is not null)"), and a
// fragment that is nothing but identifiers and punctuation (", " + col) — the
// composition that uses the fragment is judged on its own text. Reported:
// prose in capitals that opens a line or follows an operand with a clause
// word ("%s WHERE %s is not absent"). Both are the price of judging text
// without types: the extractor does not know where a string flows.
func sqlBearing(text string) bool {
	t := strings.ReplaceAll(text, sqlextract.Placeholder, " "+sqlextract.Placeholder+" ")
	token := hasSQLToken(text)
	if looksLikeSQL(t) || opensWithStatement(t) || opensWithClause(t, token) {
		return true
	}
	for _, line := range strings.FieldsFunc(t, func(r rune) bool { return r == '\n' || r == ';' }) {
		if opensWithStatement(line) || opensWithClause(line, token) {
			return true
		}
	}
	for rest := t; ; {
		i := strings.Index(rest, sqlextract.Placeholder)
		if i < 0 {
			break
		}
		rest = rest[i+len(sqlextract.Placeholder):]
		after := strings.TrimLeft(rest, " \t\r\n,()")
		if (looksLikeSQL(after) && opensInCapitals(after)) || opensWithStatement(after) || opensWithClause(after, token) {
			return true
		}
	}
	return token
}

// opensWithStatement reports whether s opens with a statement keyword written
// in capitals and followed by more of the statement: "SELECT count(*)" or
// "SELECT pg_advisory_lock(" + key + ")", which carry no structural keyword
// for looksLikeSQL to find.
func opensWithStatement(s string) bool {
	s = stripLeading(s)
	i := 0
	for i < len(s) && s[i] >= 'A' && s[i] <= 'Z' {
		i++
	}
	if i == 0 || !(sqlStartKeywords[s[:i]] || sqlUtilityKeywords[s[:i]]) || i == len(s) || (s[i] != ' ' && s[i] != '\t' && s[i] != '\n') {
		return false
	}
	rest := strings.TrimLeft(s[i:], " \t\r\n")
	if need := sqlStatementSecondWord[s[:i]]; need != "" {
		word, _, _ := nextWord(rest)
		return strings.ToUpper(word) == need && strings.HasPrefix(strings.ToUpper(rest), need)
	}
	return rest != "" && (isWordChar(rest[0]) || rest[0] == '(' || rest[0] == '*')
}

// sqlUtilityKeywords open a statement looksLikeSQL does not know, because it
// carries no structural keyword to corroborate it: RESET ROLE, COPY t FROM
// stdin, LOCK TABLE. Only in capitals, through opensWithStatement. DO is not
// one: "DO NOT EDIT" heads generated files.
var sqlUtilityKeywords = map[string]bool{
	"RESET": true, "COPY": true, "LOCK": true, "CALL": true, "VACUUM": true, "ANALYZE": true,
}

// sqlStatementSecondWord is the word a statement keyword is always followed by
// in SQL. "DELETE " + n is a PostgreSQL command tag, and a test faking a
// driver result spells one; "DELETE FROM" is a statement.
var sqlStatementSecondWord = map[string]string{"DELETE": "FROM", "INSERT": "INTO"}

// opensInCapitals reports whether s, after leading whitespace and comments,
// opens with a capital letter. A statement found after an operand counts only
// when its keyword is written in capitals: a template's prose continues an
// operand with "with … as" as readily as SQL does.
func opensInCapitals(s string) bool {
	s = stripLeading(s)
	return s != "" && s[0] >= 'A' && s[0] <= 'Z'
}

// sqlConnectives open a clause in SQL and a clause in English, and in other
// query languages too — a Cloud Logging filter joins its terms with AND. One
// counts only beside a SQL token, whatever its case.
var sqlConnectives = map[string]bool{"AND": true, "OR": true, "NOT": true}

// sqlClauseKeywords are the words that open a clause continuing a statement
// composed elsewhere. sqlStructuralKeywords plus the connectives, set
// operators, join qualifiers and locking clause that only ever follow one.
var sqlClauseKeywords = map[string]bool{
	"AND": true, "OR": true, "NOT": true, "OFFSET": true, "UNION": true,
	"EXCEPT": true, "INTERSECT": true, "FOR": true, "WINDOW": true,
	"LEFT": true, "RIGHT": true, "INNER": true, "FULL": true, "CROSS": true,
	"LATERAL": true, "EXISTS": true,
}

// opensWithClause reports whether s, after leading whitespace and comments,
// opens with a clause keyword: written in capitals, or anywhere when the text
// carries a SQL token (token).
func opensWithClause(s string, token bool) bool {
	s = stripLeading(s)
	i := 0
	for i < len(s) && isAlpha(s[i]) {
		i++
	}
	if i == 0 || (i < len(s) && isWordChar(s[i])) {
		return false
	}
	word := s[:i]
	up := strings.ToUpper(word)
	if !sqlClauseKeywords[up] && !sqlStructuralKeywords[up] {
		return false
	}
	if sqlConnectives[up] {
		return token
	}
	return word == up || token
}

// hasSQLToken reports whether s holds a token only SQL spells: a type cast
// (::name), a positional parameter in parameter position ($1 followed by a
// cast, a comma, a closing parenthesis or the end; or a composed one cast,
// "$" + strconv.Itoa(n) + "::uuid"), IS NULL or IS NOT NULL in capitals,
// = ANY(, or COALESCE( or NULLIF( in capitals. A bare "$" + n is not one: it
// is how money is written too. s is the text as reassembled, before
// sqlBearing spaces the placeholder out, so "$fw_expr::text" still reads as
// one token.
func hasSQLToken(s string) bool {
	for i := 0; i+2 < len(s); i++ {
		if s[i] == ':' && s[i+1] == ':' && (isAlpha(s[i+2]) || s[i+2] == '_') {
			return true
		}
	}
	for i := 0; i < len(s); i++ {
		if s[i] != '$' {
			continue
		}
		rest := s[i+1:]
		if strings.HasPrefix(rest, sqlextract.Placeholder+"::") {
			return true
		}
		j := 0
		for j < len(rest) && rest[j] >= '0' && rest[j] <= '9' {
			j++
		}
		if j == 0 || rest[0] == '0' {
			continue
		}
		next := strings.TrimLeft(rest[j:], " ")
		if next == "" || strings.HasPrefix(next, "::") || strings.ContainsRune(",)=<>", rune(next[0])) ||
			strings.HasPrefix(next, sqlextract.Placeholder) {
			return true
		}
		if prev := strings.TrimRight(s[:i], " "); prev != "" && strings.ContainsRune("=<>(,", rune(prev[len(prev)-1])) {
			return true
		}
	}
	for _, tok := range []string{"IS NULL", "IS NOT NULL", "IS DISTINCT FROM", "IS NOT DISTINCT FROM", "= ANY(", "=ANY(", "COALESCE(", "NULLIF(", "(SELECT ", "( SELECT "} {
		if strings.Contains(s, tok) {
			return true
		}
	}
	return false
}
