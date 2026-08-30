package sqlparse

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	pg "github.com/pganalyze/pg_query_go/v6"
	"gopkg.in/yaml.v3"
)

// sql/locking-target answers the question sql/locking-select-order cannot:
// WHICH relation is locked, and how strongly (#37).
//
// The two are different hazards and neither covers the other. Ordering prevents
// two writers taking the same rows in different sequences. An exclusive lock on
// a SPECIFIC row is dangerous regardless of ordering, when another writer holds
// a child row and wants that row at a weaker strength — the FK FOR KEY SHARE
// cycle. A single-row lock is just as deadly there, so the ORDER BY rule
// correctly passes it and the hazard goes unpoliced.
//
// ALIAS RESOLUTION IS THE POINT, and it is why this cannot be a pattern rule.
// `FOR UPDATE OF p` names an identifier, not a table; binding `p` to
// `app.projects` needs the statement's FROM clause. A rule that banned the two
// tokens' co-occurrence fires on every statement that locks something ELSE in a
// query mentioning the guarded table — and a gate with four false positives per
// true one gets weakened or switched off.
//
// The downstream replacement for this rule is the worked example: a regex with
// whole-file require_absent guards, measured 4/5 correct, where the fifth is a
// genuine exclusive lock on the guarded row that it misses. That trade was
// forced by the missing vocabulary, not chosen carelessly.
type lockingTarget struct {
	table *regexp.Regexp
	// schema is nil when unconfigured, which guards EVERY schema. See
	// guardsSchema for why that is the default and why an unqualified
	// relation is reported under a configured one.
	schema   *regexp.Regexp
	strength map[pg.LockClauseStrength]bool
}

// strengthNames maps the config vocabulary to PostgreSQL's lock clause
// strengths. Spelled as the SQL reads rather than as the enum does, because the
// operator writing the rule is reading the SQL.
var strengthNames = map[string]pg.LockClauseStrength{
	"update":        pg.LockClauseStrength_LCS_FORUPDATE,
	"no-key-update": pg.LockClauseStrength_LCS_FORNOKEYUPDATE,
	"share":         pg.LockClauseStrength_LCS_FORSHARE,
	"key-share":     pg.LockClauseStrength_LCS_FORKEYSHARE,
}

type lockingTargetParams struct {
	Table    string   `yaml:"table"`
	Schema   string   `yaml:"schema"`
	Strength []string `yaml:"strength"`
}

// qualifiedTable recognises a params.table written as `schema.relation`: two
// plain identifiers joined by a dot, the dot optionally escaped. params.table
// is matched against the relation NAME alone, so such a value can never fire —
// `public.projects` does not match the relname `projects`, not even in the
// statement that spells it exactly. Refusing it is the difference between a
// config error at exit 2 and a rule that silently matches nothing.
//
// Anything carrying real pattern syntax (`proj.*`, `^projects$`,
// `projects|pages`) is a regex over the relname and passes through.
var qualifiedTable = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_$]*\\?\.[A-Za-z_][A-Za-z0-9_$]*$`)

func newLockingTarget(node *yaml.Node) (rules.Checker, error) {
	var p lockingTargetParams
	if err := rules.DecodeParams(node, &p); err != nil {
		return nil, err
	}
	if p.Table == "" {
		return nil, fmt.Errorf("sql/locking-target: params.table is required")
	}
	if qualifiedTable.MatchString(p.Table) {
		schema, relname, _ := strings.Cut(strings.ReplaceAll(p.Table, `\`, ""), ".")
		return nil, fmt.Errorf(
			"sql/locking-target: params.table %q is schema-qualified, and params.table is a regex over the relation NAME alone — it would match nothing, including the statement it names. Write params.schema: %q and params.table: %q",
			p.Table, schema, relname)
	}
	re, err := regexp.Compile(p.Table)
	if err != nil {
		return nil, fmt.Errorf("sql/locking-target: params.table: %w", err)
	}
	// An absent schema guards every schema; an empty one is the same statement
	// spelled differently, so both leave the matcher nil rather than compiling
	// an empty regex that would read as a decision.
	var schemaRE *regexp.Regexp
	if p.Schema != "" {
		schemaRE, err = regexp.Compile(p.Schema)
		if err != nil {
			return nil, fmt.Errorf("sql/locking-target: params.schema: %w", err)
		}
	}
	// An empty strength list would match nothing, which is a rule that cannot
	// fire — the vacuity this repo reports everywhere else. Require it.
	if len(p.Strength) == 0 {
		return nil, fmt.Errorf("sql/locking-target: params.strength is required (%s)", knownStrengths())
	}
	want := map[pg.LockClauseStrength]bool{}
	for _, s := range p.Strength {
		v, ok := strengthNames[s]
		if !ok {
			return nil, fmt.Errorf("sql/locking-target: unknown strength %q (want %s)", s, knownStrengths())
		}
		want[v] = true
	}
	return &lockingTarget{table: re, schema: schemaRE, strength: want}, nil
}

func knownStrengths() string {
	names := make([]string, 0, len(strengthNames))
	for n := range strengthNames {
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func (c *lockingTarget) Cost() rules.Cost { return rules.CostFast }

// CheckFile reports every locking clause that takes one of the configured
// strengths on a relation whose NAME matches table and whose SCHEMA the rule
// guards, after resolving the identifier the clause names through the
// statement's FROM bindings.
func (c *lockingTarget) CheckFile(f *scan.File) ([]rules.Match, error) {
	stmts, err := lockingStatements(f)
	if err != nil {
		return nil, err
	}
	var ms []rules.Match
	for _, s := range stmts {
		for _, sel := range lockingSelects(s.Node) {
			bindings := relationBindings(sel.GetFromClause())
			for _, id := range c.lockedAtGuardedStrength(sel) {
				// An identifier with no binding is a relation this statement
				// does not introduce — a CTE name, or a reference the FROM
				// clause does not carry. Not resolvable, so not reported:
				// guessing here is how a rule earns its false positives.
				b, ok := bindings[id]
				if !ok || !c.table.MatchString(b.relname) || !c.guardsSchema(b) {
					continue
				}
				ms = append(ms, rules.Match{
					Line: s.Line,
					Message: fmt.Sprintf(
						"locking SELECT takes a guarded lock on %s (via %q) — an exclusive lock on this row can deadlock against a writer holding a child row at a weaker strength",
						b.qualified(), id),
				})
			}
		}
	}
	return ms, nil
}

// lockedAtGuardedStrength returns the identifiers sel locks at one of the
// configured strengths. A bare FOR UPDATE (no OF) locks every base relation in
// the FROM clause, which is what lockedRelations already models.
func (c *lockingTarget) lockedAtGuardedStrength(sel *pg.SelectStmt) []string {
	var out []string
	for _, lcNode := range sel.GetLockingClause() {
		lc := lcNode.GetLockingClause()
		if lc == nil || !c.strength[lc.GetStrength()] {
			continue
		}
		named := false
		for _, rel := range lc.GetLockedRels() {
			if rv := rel.GetRangeVar(); rv != nil {
				out = append(out, rangeVarID(rv))
				named = true
			}
		}
		if !named {
			out = append(out, baseRelationIDs(sel.GetFromClause())...)
		}
	}
	return out
}

// guardsSchema reports whether b sits in a schema this rule guards.
//
// An unconfigured schema guards every schema. That is the pre-#294 behaviour
// and the right default: most SQL never spells a schema, and a guard that
// narrowed itself by default would go quiet on the corpus it was pointed at.
//
// A relation the SOURCE does not qualify has no schema here at all —
// PostgreSQL resolves a bare `projects` through search_path at execution time,
// which no parse tree carries. It is reported rather than skipped: the guarded
// table is the likeliest thing it resolves to, and a deadlock guard that stays
// quiet on the ambiguous case misses the hazard it exists for. This is also
// what keeps the rule from firing on formatting — whether a statement spells
// its schema changes which findings NAME a schema, never whether a qualified
// relation in the guarded schema is caught.
func (c *lockingTarget) guardsSchema(b relationBinding) bool {
	if c.schema == nil || b.schema == "" {
		return true
	}
	return c.schema.MatchString(b.schema)
}

// relationBinding is what one identifier in a FROM list stands for: the
// relation's name, and the schema the source qualified it with — empty when it
// did not, which is a genuinely unknown schema and not a claim of "public".
type relationBinding struct {
	schema  string
	relname string
}

// qualified spells the relation the way the source did, so a finding on
// `other.projects` cannot be read as one on `public.projects`.
func (b relationBinding) qualified() string {
	if b.schema == "" {
		return b.relname
	}
	return b.schema + "." + b.relname
}

// relationBindings maps each identifier a FROM list introduces to the relation
// it stands for: `app.projects AS p` binds p -> app.projects, and an unaliased
// `app.projects` binds projects -> app.projects.
func relationBindings(from []*pg.Node) map[string]relationBinding {
	out := map[string]relationBinding{}
	var visit func(n *pg.Node)
	visit = func(n *pg.Node) {
		if n == nil {
			return
		}
		if rv := n.GetRangeVar(); rv != nil {
			out[rangeVarID(rv)] = relationBinding{schema: rv.GetSchemaname(), relname: rv.GetRelname()}
			return
		}
		if j := n.GetJoinExpr(); j != nil {
			visit(j.GetLarg())
			visit(j.GetRarg())
		}
	}
	for _, n := range from {
		visit(n)
	}
	return out
}

func init() { rules.Register("sql/locking-target", newLockingTarget) }
