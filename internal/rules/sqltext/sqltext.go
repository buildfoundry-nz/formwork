// Package sqltext implements the `sql/statement-predicate` rule type: a
// predicate over individual SQL statements, sourced both from `.sql` files
// (used verbatim) and from SQL embedded in Go string literals (`.go` files,
// reassembled heuristically — see sqlextract).
//
// A statement is "selected" when its text matches the required `table` regex.
// Each selected statement must satisfy every `require` token regex and match
// none of the `forbid` token regexes; a selected statement that violates the
// predicate yields one finding. Files that are neither `.go` nor `.sql` yield
// no findings. A `.go` file that fails to parse is a returned error (the engine
// turns it into an exit-2 config/engine error), never a silent pass.
//
// COMMENTS ARE NOT STATEMENT TEXT. `--` and `/* … */` comments are blanked
// before a statement is selected or judged (splitStatements, via
// preprocess.DecommentSQL), so a token carried only by a comment neither
// satisfies a `require`, selects through `table`, nor fires a `forbid`, and a
// ';' inside a comment does not terminate a statement. Comment-shaped bytes
// inside `'…'`, `"…"` and `$tag$…$tag$` are string data and survive. Without
// this, a developer's comment explaining why a column is absent satisfies the
// rule that exists to demand it — the accidental shape, not an adversarial one
// (#137, reported downstream by the validating port). No `require` regex can
// express it instead: these are RE2 patterns, which have no negative lookahead.
//
// NEITHER IS A ';' INSIDE A STRING LITERAL A STATEMENT BOUNDARY (#139). The
// statement split is lexical rather than byte-wise WHERE IT CAN BE: `'…'`,
// `E'…'`, `"…"` and `$tag$…$tag$` are skipped whole, so a semicolon in a
// delimiter constant, an error template or a string_agg separator no longer
// truncates the statement around it. That was a FALSE POSITIVE — the rule
// failed conforming code — and it silently produced a second, unselectable
// fragment out of the tail. Text the scan cannot lex (an unterminated literal)
// or cannot place (a sqlextract fragment that may open mid-literal) keeps the
// byte-wise split instead, because a wrong skip merges statements and a merged
// statement is a statement not judged; splitStatements states that trade in
// full. Why not the real grammar: sqlparse hands whole text to pg_query and
// takes ITS splitting, but this rule type must also judge the fragments
// sqlextract yields for a composition it could not reassemble, which do not
// parse; and the package doc there records that nothing outside sqlparse
// imports go-pgquery.
//
// HEURISTIC LIMITS remain, and are sqlextract's: SQL composed by fmt.Sprintf,
// by concatenation with a non-literal operand, or through strings.Builder is
// not reassembled, so a statement built that way is split into fragments or
// dropped entirely. Constant folding is the boundary — see sqlextract's own
// HEURISTIC LIMITS block. One further limit lives here: `require` is RE2
// MatchString over the whole statement, so a single data-modifying CTE carrying
// two INSERT arms is satisfied by the compliant arm.
package sqltext

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/buildfoundry-nz/formwork/internal/preprocess"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"github.com/buildfoundry-nz/formwork/internal/sqlextract"
	"gopkg.in/yaml.v3"
)

type params struct {
	Table   string   `yaml:"table"`
	Require []string `yaml:"require"`
	Forbid  []string `yaml:"forbid"`
}

type predicate struct {
	table   *regexp.Regexp
	require []*regexp.Regexp
	forbid  []*regexp.Regexp
}

func compileAll(kind string, pats []string) ([]*regexp.Regexp, error) {
	out := make([]*regexp.Regexp, 0, len(pats))
	for i, p := range pats {
		if p == "" {
			return nil, fmt.Errorf("sql/statement-predicate: %s[%d] is empty", kind, i)
		}
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("sql/statement-predicate: invalid %s regex %q: %w", kind, p, err)
		}
		out = append(out, re)
	}
	return out, nil
}

func newPredicate(node *yaml.Node) (rules.Checker, error) {
	var p params
	if err := rules.DecodeParams(node, &p); err != nil {
		return nil, err
	}
	if p.Table == "" {
		return nil, errors.New("sql/statement-predicate: params.table is required")
	}
	table, err := regexp.Compile(p.Table)
	if err != nil {
		return nil, fmt.Errorf("sql/statement-predicate: invalid table regex %q: %w", p.Table, err)
	}
	if len(p.Require) == 0 && len(p.Forbid) == 0 {
		return nil, errors.New("sql/statement-predicate: at least one of params.require or params.forbid is required")
	}
	require, err := compileAll("require", p.Require)
	if err != nil {
		return nil, err
	}
	forbid, err := compileAll("forbid", p.Forbid)
	if err != nil {
		return nil, err
	}
	return &predicate{table: table, require: require, forbid: forbid}, nil
}

// statement is one candidate SQL statement plus the source line it starts on.
type statement struct {
	text string
	line int
}

func (c *predicate) CheckFile(f *scan.File) ([]rules.Match, error) {
	var stmts []statement
	switch sqlextract.FileKind(f.Path()) {
	case "sql":
		content, err := f.Content()
		if err != nil {
			return nil, err
		}
		// A .sql file is whole text: it begins outside any literal.
		stmts = splitStatements(string(content), 1, true)
	case "go":
		content, err := f.Content()
		if err != nil {
			return nil, err
		}
		stmts, err = goStatements(f.Path(), content)
		if err != nil {
			return nil, err
		}
	default:
		// Not this analyzer's language.
		return nil, nil
	}

	var matches []rules.Match
	for _, s := range stmts {
		if !c.table.MatchString(s.text) {
			continue
		}
		reasons := c.violations(s.text)
		if len(reasons) == 0 {
			continue
		}
		matches = append(matches, rules.Match{
			Line: s.line,
			Message: fmt.Sprintf("SQL statement on table /%s/ %s: %q",
				c.table.String(), strings.Join(reasons, "; "), snippet(s.text)),
		})
	}
	return matches, nil
}

// violations returns the human-readable reasons a selected statement breaks the
// predicate, in a deterministic order (missing requires first, then present
// forbids). An empty result means the statement is compliant.
func (c *predicate) violations(text string) []string {
	var reasons []string
	for _, re := range c.require {
		if !re.MatchString(text) {
			reasons = append(reasons, "missing required /"+re.String()+"/")
		}
	}
	for _, re := range c.forbid {
		if re.MatchString(text) {
			reasons = append(reasons, "contains forbidden /"+re.String()+"/")
		}
	}
	return reasons
}

// splitStatements breaks SQL text into statements on ';', discarding blank
// segments. baseLine is the 1-based line the text begins on; each statement's
// reported line is that of its first non-whitespace character.
//
// Comments are blanked first (#137). SQL comments are not statement text: a
// token that appears only in a comment must not satisfy a `require`, select a
// statement through `table`, or fire a `forbid`, and a ';' inside a comment is
// not a statement terminator. This is the ONE seam both sources funnel through
// — the `.sql` path calls it on file content, goStatements calls it on each
// reassembled Go candidate — so neither can be left uncovered.
//
// STRING LITERALS ARE NOT TERMINATORS EITHER (#139). A ';' inside `'…'`, an
// escape string `E'…'`, a quoted identifier `"…"` or a dollar-quoted body
// `$tag$…$tag$` is DATA, and reading it as a terminator truncated the
// statement — failing code that conforms, and leaving the tail as a fragment no
// rule ever selected. So the scan skips over literals rather than scanning
// their bytes, using sqlLiteralEnd.
//
// SKIPPING IS ONLY SOUND WHERE THE SCAN KNOWS WHICH BYTES ARE DATA, and it does
// not always. Reading a byte as data that is really code MERGES the statement
// after it into the one before, and a merged statement is judged once against a
// `require` any part of it can satisfy — so a governed statement stops being
// reported at all. That is the trade running the wrong way: #139 was a false
// positive, this is a pass the check did not earn. Two conditions decide it,
// and either one drops the whole text back to the pre-#139 byte-wise split:
//
//   - startsInCode is false. The caller cannot promise the text begins outside
//     a literal — it is a sqlextract fragment of a composition that could not
//     be reassembled (Candidate.Partial), so its very first quote may be a
//     CLOSING one and every quote after it pairs one out of phase.
//   - the text does not lex: some literal never closes (sqlLiteralEnd reports
//     -1). The fallback is then whole-text and not merely from that offset on,
//     because one unmatched quote puts the pairing of every OTHER quote in the
//     text in question too — pair them the other way and a literal the scan
//     skipped whole was code carrying a terminator.
//
// Byte-wise splitting can only ADD split points, never remove one, so the
// fallback set is never coarser than the statements this rule judged before
// #139 — it over-splits, which shows up as the #139 false positive, and it
// cannot hide a statement.
//
// DecommentSQL is length- and newline-preserving, so every offset and line
// number computed below is the same one the raw text would have produced. That
// is load-bearing rather than incidental: it is why a finding's reported line
// still points at the statement and not at wherever a collapsed comment left it.
// It is also why skipping a literal here is safe: comment-shaped bytes are
// already gone, so no blanked comment can carry an unbalanced quote into this
// scan, and every quote it sees is one the SQL really has.
func splitStatements(content string, baseLine int, startsInCode bool) []statement {
	content = string(preprocess.DecommentSQL([]byte(content)))
	if startsInCode {
		if out, ok := splitScan(content, baseLine, true); ok {
			return out
		}
	}
	out, _ := splitScan(content, baseLine, false)
	return out
}

// splitScan splits content on ';' and reports whether the result may be used.
// With lexical set it skips string literals; it returns ok=false — and no
// statements — when it meets a literal that never closes, because the split it
// could produce from there merges statements rather than separating them. With
// lexical unset every byte is code, the scan cannot fail, and ok is always true.
func splitScan(content string, baseLine int, lexical bool) ([]statement, bool) {
	var out []statement
	line := baseLine
	segStart := 0
	segStartLine := baseLine
	flush := func(end int) {
		seg := content[segStart:end]
		if strings.TrimSpace(seg) == "" {
			return
		}
		l := segStartLine
		for _, r := range seg {
			if r == '\n' {
				l++
				continue
			}
			if r == ' ' || r == '\t' || r == '\r' {
				continue
			}
			break
		}
		out = append(out, statement{text: seg, line: l})
	}
	for i := 0; i < len(content); {
		switch content[i] {
		case ';':
			flush(i)
			segStart = i + 1
			segStartLine = line
			i++
		case '\n':
			line++
			i++
		default:
			if !lexical {
				i++
				continue
			}
			end, opens := sqlLiteralEnd(content, i)
			switch {
			case !opens:
				i++
			case end < 0:
				// Unterminated: refuse the whole scan rather than guess.
				return nil, false
			default:
				// A literal may span lines; its newlines still advance the
				// counter, or every statement after a multi-line literal
				// reports a line earlier than it starts on.
				line += strings.Count(content[i:end], "\n")
				i = end
			}
		}
	}
	flush(len(content))
	return out, true
}

// SQL STRING-LITERAL LEXING, SECOND COPY — read this before touching it.
//
// preprocess.sqlScanComments already models these same four literal forms, and
// #139 asked for one shared definition rather than two. It stayed two here for
// a scope reason, not a design one: that lexer is unexported and lives in a
// package this change was not permitted to open. THE TWO ARE THEREFORE ABLE TO
// DRIFT, which is the exact failure sqlScanComments' own header warns about,
// and closing it means exporting a statement-boundary entry point from
// preprocess and deleting everything below. Until then, a change to
// PostgreSQL literal handling in either file belongs in both.
//
// The comment states are deliberately absent: splitStatements runs over
// already-decommented text, so `--` and `/* … */` cannot appear as anything but
// blanks by the time these functions see them.

// sqlLiteralEnd reports whether a SQL string literal opens at s[i] and, if so,
// the offset just past its close — or -1 when it never closes.
//
// The -1 is the whole point of the second return being separate from the first:
// "no literal opens here" and "a literal opens here and I cannot tell you where
// it ends" are different answers, and only the caller can decide what to do
// with the second. preprocess's SQL comment lexer resolves the same case by
// letting an unterminated literal swallow the rest of the input, which is
// fail-CLOSED there — swallowed text stays code, so no `require` can be
// satisfied by a comment. Here the consequence inverts: swallowed text becomes
// part of one statement, so a `require` IS satisfied across bytes we cannot
// prove are code and every governed statement after the quote goes unjudged.
// Same direction, opposite outcome; splitStatements therefore falls back to a
// byte-wise split instead of importing that direction.
func sqlLiteralEnd(s string, i int) (int, bool) {
	switch {
	case s[i] == '\'':
		return sqlQuotedEnd(s, i+1, '\'', false), true
	case s[i] == '"':
		return sqlQuotedEnd(s, i+1, '"', false), true
	case (s[i] == 'E' || s[i] == 'e') && i+1 < len(s) && s[i+1] == '\'' && !sqlIdentByte(sqlPrevByte(s, i)):
		// E'…' takes backslash escapes; a plain '…' does not. The adjacency
		// guard keeps the trailing 'e' of an identifier (`table'`… never
		// happens, but `nde'x'` would) from opening an escape string.
		return sqlQuotedEnd(s, i+2, '\'', true), true
	case s[i] == '$' && !sqlIdentByte(sqlPrevByte(s, i)):
		// A '$' glued onto an identifier character continues that identifier
		// (`col$a$name` is ONE identifier) and opens nothing; so does `$1`,
		// whose tag would have to start with a digit.
		if tag, ok := sqlDollarTag(s, i); ok {
			return sqlDollarEnd(s, i+len(tag), tag), true
		}
	}
	return 0, false
}

// sqlQuotedEnd scans a quote-delimited literal whose body starts at j, closed
// by q. A doubled delimiter (two single or two double quotes) is an escaped
// one and stays inside. When
// esc is set (an E'…' string) a backslash escapes the next byte. Returns the
// offset just past the closing delimiter, or -1 when unterminated.
func sqlQuotedEnd(s string, j int, q byte, esc bool) int {
	for j < len(s) {
		switch {
		case esc && s[j] == '\\':
			j += 2 // may pass len(s); the loop condition catches it
		case s[j] == q:
			if j+1 < len(s) && s[j+1] == q {
				j += 2
				continue
			}
			return j + 1
		default:
			j++
		}
	}
	return -1
}

// sqlDollarTag returns the full dollar-quote delimiter opening at s[at] (which
// must be '$') — "$$" for an empty tag, or "$name$" — and whether one opens at
// all. A non-empty tag follows unquoted-identifier rules: a leading letter or
// underscore, then letters, digits or underscores.
func sqlDollarTag(s string, at int) (string, bool) {
	j := at + 1
	if j < len(s) && sqlIdentStartByte(s[j]) {
		for j < len(s) && sqlIdentByte(s[j]) {
			j++
		}
	}
	if j < len(s) && s[j] == '$' {
		return s[at : j+1], true
	}
	return "", false
}

// sqlDollarEnd returns the offset just past the closing tag of a dollar-quoted
// body starting at j, or -1 when unterminated. The body takes no escapes, so
// the next occurrence of the tag closes it.
func sqlDollarEnd(s string, j int, tag string) int {
	if k := strings.Index(s[j:], tag); k >= 0 {
		return j + k + len(tag)
	}
	return -1
}

// sqlPrevByte returns the byte before offset i, or 0 at the start of input
// (which is not an identifier byte, so a leading '$' or 'E' is at a token
// boundary).
func sqlPrevByte(s string, i int) byte {
	if i <= 0 {
		return 0
	}
	return s[i-1]
}

func sqlIdentByte(c byte) bool {
	return c == '_' || ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z') || ('0' <= c && c <= '9')
}

func sqlIdentStartByte(c byte) bool {
	return c == '_' || ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z')
}

// goStatements reassembles SQL held in Go string literals (via sqlextract) and
// splits each reassembled candidate into statements. The reassembled literal
// loses reliable interior line positions, so every statement derived from one
// candidate reports the line of the string expression it came from. A Go parse
// failure is returned as an error.
func goStatements(path string, src []byte) ([]statement, error) {
	// The unresolved Sites are discarded HERE, deliberately (#75). They are no
	// longer unread: lint's escape-hatch census asks FromGo the same question
	// over the files this rule governs and enumerates every composition the
	// extractor could not read, with its reason.
	//
	// Reported by lint rather than by this rule because an unreadable query is
	// not a VIOLATION — it is a gap in coverage, and a check that failed on one
	// would fail on ordinary dynamic SQL the corpus never intended to police.
	// The cost of that split is that `check` alone still shows no trace; run
	// `formwork lint` to see what was not analysed.
	cands, _, err := sqlextract.FromGo(path, src)
	if err != nil {
		return nil, fmt.Errorf("sql/statement-predicate: %w", err)
	}
	var out []statement
	for _, cand := range cands {
		// Partial marks a fragment of a composition sqlextract could not
		// reassemble; it may begin inside a string literal, so its quote
		// pairing proves nothing (see splitStatements).
		for _, s := range splitStatements(cand.Text, cand.Line, !cand.Partial) {
			// interior line discarded on purpose (see doc): every statement from
			// one Go candidate reports that candidate's line.
			out = append(out, statement{text: s.text, line: cand.Line})
		}
	}
	return out, nil
}

// snippet renders a compact, whitespace-collapsed preview of a statement.
func snippet(text string) string {
	s := strings.Join(strings.Fields(text), " ")
	const max = 80
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}

func init() {
	rules.Register("sql/statement-predicate", newPredicate)
}
