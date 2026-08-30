//go:build ignore

// Faithfully ported to Go from checks/check.py (itself extracted from
// scripts/check-schema-fk-delete-path-indexed.sh, #8259): parse the committed
// schema snapshot for FKs + indexes, seed the delete-closure from
// `DELETE FROM palletra./platform.` in the request-serving Go (excluding
// _test.go and integration-tagged files), close it over ON DELETE CASCADE, and
// require a covering index on every FK whose parent is in the closure.
// Invoked as `go run checks/check.go <root>`.
package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

type cover struct {
	lead   string
	usable bool
}

type fk struct {
	child  string
	cols   []string
	parent string
	action string
	cname  string
}

func balanced(s string, openParenAt int) (string, int) {
	depth := 0
	for i := openParenAt; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return s[openParenAt+1 : i], i + 1
			}
		}
	}
	panic(fmt.Sprintf("unbalanced parentheses in snapshot near offset %d", openParenAt))
}

var (
	ascDescRe = regexp.MustCompile(`(?i)\s+(ASC|DESC)$`)
	nullsRe   = regexp.MustCompile(`(?i)\s+NULLS\s+(FIRST|LAST)$`)
	predStrip = regexp.MustCompile(`[()\s]`)
)

func splitKeys(cols string) []string {
	depth := 0
	var parts []string
	var cur strings.Builder
	for _, ch := range cols {
		switch {
		case ch == '(':
			depth++
			cur.WriteRune(ch)
		case ch == ')':
			depth--
			cur.WriteRune(ch)
		case ch == ',' && depth == 0:
			parts = append(parts, cur.String())
			cur.Reset()
		default:
			cur.WriteRune(ch)
		}
	}
	parts = append(parts, cur.String())
	keys := make([]string, 0, len(parts))
	for _, t := range parts {
		t = strings.TrimSpace(strings.Trim(strings.TrimSpace(t), `"`))
		t = ascDescRe.ReplaceAllString(t, "")
		t = nullsRe.ReplaceAllString(t, "")
		keys = append(keys, strings.TrimSpace(t))
	}
	return keys
}

func buildConstraint(src string) string {
	for _, line := range strings.Split(src, "\n") {
		s := strings.TrimSpace(line)
		if s == "" || strings.HasPrefix(s, "//go:build") || strings.HasPrefix(s, "//") || strings.HasPrefix(s, "/*") || strings.HasPrefix(s, "*") {
			if strings.HasPrefix(s, "//go:build") {
				return strings.TrimSpace(strings.TrimPrefix(s, "//go:build"))
			}
			continue
		}
		break
	}
	return ""
}

var (
	tokRe        = regexp.MustCompile(`&&|\|\||[!()]|[A-Za-z0-9_.]+`)
	identPattern = regexp.MustCompile(`^[A-Za-z0-9_.]+$`)
)

// requiresIntegrationTag reports whether the //go:build expression can only be
// satisfied when the integration tag is set.
func requiresIntegrationTag(expr string) bool {
	if expr == "" {
		return false
	}
	toks := tokRe.FindAllString(expr, -1)
	hasIntegration := false
	otherSet := map[string]bool{}
	for _, t := range toks {
		if !identPattern.MatchString(t) {
			continue
		}
		if t == "integration" {
			hasIntegration = true
		} else {
			otherSet[t] = true
		}
	}
	if !hasIntegration {
		return false
	}
	others := make([]string, 0, len(otherSet))
	for id := range otherSet {
		others = append(others, id)
	}
	sort.Strings(others)

	evaluate := func(assign map[string]bool) bool {
		pos := 0
		var parseOrExpr func() bool
		var parseAndExpr func() bool
		var parseUnaryExpr func() bool
		parseOrExpr = func() bool {
			v := parseAndExpr()
			for pos < len(toks) && toks[pos] == "||" {
				pos++
				rhs := parseAndExpr()
				v = v || rhs
			}
			return v
		}
		parseAndExpr = func() bool {
			v := parseUnaryExpr()
			for pos < len(toks) && toks[pos] == "&&" {
				pos++
				rhs := parseUnaryExpr()
				v = v && rhs
			}
			return v
		}
		parseUnaryExpr = func() bool {
			if pos < len(toks) && toks[pos] == "!" {
				pos++
				return !parseUnaryExpr()
			}
			if pos < len(toks) && toks[pos] == "(" {
				pos++
				v := parseOrExpr()
				if pos < len(toks) && toks[pos] == ")" {
					pos++
				}
				return v
			}
			tok := toks[pos]
			pos++
			return assign[tok]
		}
		return parseOrExpr()
	}

	for combo := 0; combo < (1 << uint(len(others))); combo++ {
		assign := map[string]bool{"integration": false}
		for i, name := range others {
			assign[name] = combo&(1<<uint(i)) != 0
		}
		if evaluate(assign) {
			return false
		}
	}
	return true
}

func main() {
	root := os.Args[1]
	snapPath := filepath.Join(root, "freightworks/services/core-api/migrations/schema.snapshot.sql")
	data, err := os.ReadFile(snapPath)
	// DEFAULT-DENY, which the Python original got for free: an uncaught
	// exception was exit 1 there, so the only absence it tolerated was the one
	// it tested for. Go hands every failure back as one `err`, and folding all
	// of them into the skip prints "no snapshot at <path>, skipping" over a
	// path the tree DOES carry — a false sentence, at exit 0, with not one FK
	// judged (#262 finding 1). Absent is a skip; unreadable is a refusal.
	if os.IsNotExist(err) {
		fmt.Printf("[check-schema-fk-delete-path-indexed] no snapshot at %s, skipping\n", snapPath)
		os.Exit(0)
	}
	if err != nil {
		fmt.Printf("[check-schema-fk-delete-path-indexed] ERROR — the schema snapshot at %s is present but unreadable, so every FK it declares would go unjudged: %v\n", snapPath, err)
		os.Exit(1)
	}
	// Python's open().read() decoded strictly and raised on any byte it could
	// not decode, which was exit 1. Go's string(data) keeps them, and the FK and
	// index regexes below go on matching the valid remainder — so the run
	// reports on the part of the snapshot it could read and says nothing at all
	// about the part it could not (#262 finding 3).
	if !utf8.Valid(data) {
		fmt.Printf("[check-schema-fk-delete-path-indexed] ERROR — the schema snapshot at %s is not valid UTF-8, so the FK and index regexes would judge only the decodable remainder of it\n", snapPath)
		os.Exit(1)
	}
	snap := string(data)

	covers := map[string][]cover{}
	indexRe := regexp.MustCompile(`CREATE (?:UNIQUE )?INDEX (\w+) ON (?:ONLY )?([\w.]+) USING (\w+) `)
	for _, m := range indexRe.FindAllStringSubmatchIndex(snap, -1) {
		tbl := snap[m[4]:m[5]]
		method := snap[m[6]:m[7]]
		openParenAt := m[1] - 1 + strings.Index(snap[m[1]-1:], "(")
		cols, after := balanced(snap, openParenAt)
		tail := snap[after : after+strings.Index(snap[after:], ";")]
		lead := splitKeys(cols)[0]
		usable := method == "btree"
		if up := strings.ToUpper(tail); strings.Contains(up, "WHERE") {
			pred := predStrip.ReplaceAllString(strings.SplitN(up, "WHERE", 2)[1], "")
			usable = usable && pred == strings.ToUpper(lead)+"ISNOTNULL"
		}
		covers[tbl] = append(covers[tbl], cover{lead, usable})
	}

	pkRe := regexp.MustCompile(`(?s)ALTER TABLE (?:ONLY )?([\w.]+)\s+ADD CONSTRAINT \w+ (?:PRIMARY KEY|UNIQUE) \(([^)]*)\);`)
	for _, m := range pkRe.FindAllStringSubmatch(snap, -1) {
		covers[m[1]] = append(covers[m[1]], cover{splitKeys(m[2])[0], true})
	}

	var fks []fk
	fkRe := regexp.MustCompile(`(?s)ALTER TABLE (?:ONLY )?([\w.]+)\s+ADD CONSTRAINT (\w+) FOREIGN KEY \(([^)]*)\) REFERENCES ([\w.]+)\(([^)]*)\)([^;]*);`)
	for _, m := range fkRe.FindAllStringSubmatch(snap, -1) {
		tail := strings.Join(strings.Fields(m[6]), " ")
		action := "NO ACTION"
		if strings.Contains(tail, "ON DELETE CASCADE") {
			action = "CASCADE"
		} else if strings.Contains(tail, "ON DELETE SET NULL") {
			action = "SET NULL"
		}
		fks = append(fks, fk{m[1], splitKeys(m[3]), m[4], action, m[2]})
	}

	deleteRe := regexp.MustCompile(`(?i)DELETE\s+FROM\s+(?:ONLY\s+)?((?:palletra|platform)\.\w+)`)

	seed := map[string]bool{}
	// Every path that NAMES Go source in the request tree and could not be read
	// is collected here instead of skipped, and the run refuses below rather
	// than seeding the delete closure from a tree it only partly saw (#262
	// finding 2). The walk and the read are ONE error path on purpose: a
	// non-regular `<name>.go` is collected by the walk and rejected by the same
	// os.ReadFile that rejects a file whose mode forbids it, so the arm that
	// reports a short seed set has a committable proof (fire-3) rather than one
	// that needs a chmod git cannot track.
	var unreadable []string
	for _, dir := range []string{"freightworks/services/core-api", "freightworks/internal"} {
		base := filepath.Join(root, dir)
		if _, err := os.Stat(base); os.IsNotExist(err) {
			// A directory the tree does not carry seeds nothing — the one
			// tolerance Python's glob had here, and it is tested for the ROOT
			// alone, so a path that goes missing deeper in the walk is an
			// error rather than a quiet gap.
			continue
		}
		var files []string
		walkRefusal := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				// Returned rather than accumulated: an error the walk itself
				// delivers is a directory it could not list, so everything
				// under it is already invisible and there is no per-path
				// answer to record. NO FIXTURE DRIVES THIS ARM — reaching it
				// needs a directory mode git does not track, and this file's
				// sibling fixtures exist because that vector cannot be
				// committed. It is here because the alternative is discarding
				// the walk's answer, which is what the port did.
				return fmt.Errorf("%s: %w", path, err)
			}
			if !strings.HasSuffix(path, ".go") {
				return nil
			}
			// A `<name>.go` that is not a regular file is COLLECTED, not
			// dropped: os.ReadFile below is the single place that decides
			// whether a path naming Go source can be read. The port returned
			// nil for every directory before it looked at the suffix at all,
			// so a directory named delete_extra.go left the seed set short in
			// complete silence.
			files = append(files, path)
			return nil
		})
		if walkRefusal != nil {
			unreadable = append(unreadable, walkRefusal.Error())
		}
		sort.Strings(files)
		for _, path := range files {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			src, err := os.ReadFile(path)
			if err != nil {
				unreadable = append(unreadable, fmt.Sprintf("%s: %v", path, err))
				continue
			}
			if requiresIntegrationTag(buildConstraint(string(src))) {
				continue
			}
			for _, m := range deleteRe.FindAllStringSubmatch(string(src), -1) {
				seed[m[1]] = true
			}
		}
	}
	if len(unreadable) > 0 {
		sort.Strings(unreadable)
		fmt.Println("[check-schema-fk-delete-path-indexed] ERROR — request-path Go file(s) could not be read, so the DELETE seed set is short and every FK below it would be judged against a closure this run cannot vouch for:")
		for _, u := range unreadable {
			fmt.Printf("  %s\n", u)
		}
		os.Exit(1)
	}

	cascadeChildren := map[string][]string{}
	for _, f := range fks {
		if f.action == "CASCADE" {
			cascadeChildren[f.parent] = append(cascadeChildren[f.parent], f.child)
		}
	}

	closure := map[string]bool{}
	var stack []string
	for s := range seed {
		closure[s] = true
		stack = append(stack, s)
	}
	for len(stack) > 0 {
		table := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, child := range cascadeChildren[table] {
			if !closure[child] {
				closure[child] = true
				stack = append(stack, child)
			}
		}
	}

	var violations []fk
	for _, f := range fks {
		if !closure[f.parent] {
			continue
		}
		covered := false
		for _, c := range covers[f.child] {
			if c.lead == f.cols[0] && c.usable {
				covered = true
				break
			}
		}
		if !covered {
			violations = append(violations, f)
		}
	}

	if len(violations) == 0 {
		fmt.Printf("[check-schema-fk-delete-path-indexed] OK — every FK referencing a request-path-deleted table is index-covered (%d table(s) in the delete closure).\n", len(closure))
		os.Exit(0)
	}

	fmt.Println("[check-schema-fk-delete-path-indexed] FAIL — foreign key(s) referencing a table the request path deletes rows from, with no covering index on the referencing side:")
	sort.Slice(violations, func(i, j int) bool {
		a, b := violations[i], violations[j]
		if a.child != b.child {
			return a.child < b.child
		}
		if strings.Join(a.cols, ",") != strings.Join(b.cols, ",") {
			return strings.Join(a.cols, ",") < strings.Join(b.cols, ",")
		}
		if a.parent != b.parent {
			return a.parent < b.parent
		}
		if a.action != b.action {
			return a.action < b.action
		}
		return a.cname < b.cname
	})
	for _, f := range violations {
		fmt.Printf("  %s (%s) -> %s  ON DELETE %s   [%s]\n", f.child, strings.Join(f.cols, ", "), f.parent, f.action, f.cname)
	}
	os.Exit(1)
}
