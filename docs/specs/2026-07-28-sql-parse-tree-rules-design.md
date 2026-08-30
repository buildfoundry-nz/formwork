# SQL parse-tree rule types + `sqlextract` promotion

Status: implemented (feat/12-sql-parse-tree-rules). Design revised after
adversarial review; further corrections (R9 + Task 6/7 review findings) landed
during subagent-driven implementation — see the plan at
this design's implementation plan.
Issue: [#12](https://github.com/buildfoundry-nz/formwork/issues/12).
Consumer driver: the validating target's SQL + migration linting lane, behind
a production deadlock report (34 deadlocks/month on one table, traced to
unordered sibling-row locking `SELECT`s).
Design spec cross-refs: §4–6 (rule registry, two-phase executor, boundaries),
§14 (the WASM/pure-Go parser-backend precedent set for dartscan).

> **Revision note.** §§4, 5.1, 5.2, 5.3, 6, 7 were revised after a four-lens
> adversarial review (9 confirmed findings, incl. 2 blockers). The changes are
> called out inline as **[R#n]** and summarised in §10.

## 1. Problem

`sql/statement-predicate` (`internal/rules/sqltext/`) is the engine's only
SQL-aware rule type, and 19 consumer rules depend on it. Three structural
limits — each visible in the current source, not a matter of better regexes —
block the highest-defect-value rules its main consumer needs:

1. **Selection is table-keyed and `table` is mandatory** (`sqltext.go:62-64`,
   gated at `:115`). Rules keyed on *statement shape* ("every locking `SELECT`
   over sibling rows must carry a deterministic `ORDER BY`") have no table
   anchor and are inexpressible.
2. **Statement splitting is a naive `;` split** — mis-splits a `;` inside a
   string/identifier literal or a dollar-quoted body (package doc says so).
3. **Predicates are text regexes.** A parse tree distinguishes a single-row
   PK lookup (`WHERE id = $1 FOR UPDATE`, ordering meaningless) from an
   unordered sibling-row lock (the deadlock shape); a text rule flags both or
   neither.

Separately, `extractGoCandidates` (the Go-string-literal → SQL reassembler)
**gives up silently**: `fmt.Sprintf`, `strings.Builder`, a `+` with a
non-literal operand, and literals spread across assignments are each dropped
with no signal. Tolerable for a table-keyed regex rule; disqualifying for a
census that must report what it could not resolve.

**Status after phase 3d — three of those four are closed, one is not.**
`sqlextract.FromGoReassembled` folds `fmt.Sprint{,f,ln}` and `+` chains with
non-literal operands (each unresolvable part becoming the `fw_expr`
placeholder), and `FromGo` reports the rest as `unresolved` Sites. **Literals
spread across assignments remain dropped silently** — `q := …` followed by
`q += " FOR UPDATE"` is two candidates, neither of them the executed
statement — as does `strings.Builder`. That is expression folding without
assignment-flow analysis, and it is the one case where a consuming rule's
SILENCE is misleading rather than merely incomplete: `sql/locking-select-order`
returns no finding for such a query however unsafe it is (verified against a
real tree — removing its `ORDER BY` still yields exit 0). Recorded on that
rule's own doc comment, since that is where it misleads. Closing it is
tracked separately and is the highest-value follow-up to this phase.

## 2. Decisions

Three load-bearing decisions were settled during brainstorming. Each revises a
choice the issue or its review left open.

### 2.1 Parser backend: `github.com/wasilibs/go-pgquery` (WASM)

The real PostgreSQL 17 grammar (`pg_query_go/v6 v6.2.2`, grammar `170004`)
compiled to WASM and run on `wazero` — pure Go, **no C toolchain**.

- **Rejected: `pganalyze/pg_query_go/v6` (cgo).** Same grammar, but cgo forfeits
  formwork's native-Windows pre-commit (the engine's stated portability
  promise) and adds a C-toolchain requirement. The issue's original pick, walked
  back by its review.
- **Rejected: a pure-Go non-real parser.** Avoids cgo *and* wasm, but is not the
  real grammar, which specifically weakens a `parses` predicate — a false
  positive there is an engineer arguing with the linter about valid Postgres,
  which is how gates lose credibility.

This follows the precedent design spec §14 already set for the *other* hard
parser: the dartscan fallback is "a tree-sitter-based backend (WASM, pure-Go
runtime) without changing rule YAML."

Version resolves on the module proxy (verified 2026-07-28: pseudo-version
`v0.0.0-20260728010200-155ebad2880e`, `go.mod` requires `wazero v1.12.0` +
`pg_query_go/v6 v6.2.2`). It is **untagged** and carries an **embedded wasm
binary** — see §7 risks. Its wazero runtime is built with a **per-runtime,
non-shared** compilation cache (`WithCompilationCache(wazero.NewCompilationCache())`),
so the wrapper must build **one** runtime and reuse it — see §5.1.

### 2.2 Type shape: separate `sql/*` types, not one vocabulary type

Each check is its own registered rule type with its own strictly-decoded param
struct — exactly the `go/*` precedent (`internal/rules/goast/analyzers.go`
registers five separate types in one package). *Not* a single
`sql/parse-predicate` type carrying a closed magic-string vocabulary in
`require:`/`forbid:`.

Rationale:

- **Strict decoding is a non-negotiable** (unknown YAML fields are exit 2, via
  `rules.DecodeParams` + `KnownFields(true)`). That guarantee attaches to the
  `type:` field and each type's param struct. A magic-string token
  (`no_sort_clause_unless_unique_key_equality`) lives *inside* params and is
  invisible to the strict decoder.
- **These checks want typed knobs.** As `go/*` types each carry their own
  (`max_lines`, `sequence`, `relation`), the SQL checks do too
  (`sql/locking-select-order` needs `unique_key_columns` and
  `order_requires_unique_key`). A closed string vocabulary has nowhere clean to
  carry a parameter; separate types give each a validated home.
- **One idiom.** Two ways to model "parse-tree predicate over a language" in one
  engine is a cost with no offsetting benefit.

Cost accepted: more boilerplate (a factory + param struct + `CheckFile` per
check). The shared parser/extractor substrate absorbs most of it.

### 2.3 First increment: substrate + `sql/parses` + `sql/locking-select-order`

Land the shared substrate (parser wrapper + `sqlextract` promotion) plus **two**
types: `sql/parses` (near-free, proves the parser wiring end-to-end and is a
real check) and `sql/locking-select-order` (the flagship — the deadlock shape
in the production report above). Defer `no-star-select`,
`has-limit`, `unnest-update-ordered` to fast-follow PRs (§8).

**Landed since:** `locking-strength` shipped as **`sql/locking-target`**
(#37) — the name changed because the rule answers WHICH relation is locked
as well as how strongly, and alias resolution against the statement's FROM
bindings is the part that cannot be done with a pattern rule.

## 3. Package layout & boundaries

```
internal/sqlextract/        NEW  — Go-literal → SQL reassembly (+ unresolved sites)
internal/rules/sqlparse/    NEW  — go-pgquery wrapper + the two sql/* rule types
internal/rules/sqltext/     EDIT — sources Go candidates from sqlextract; behavior unchanged
```

Boundaries hold to design spec §6:

- `sqlextract` knows nothing about rules (a pure Go-AST → string utility).
- `sqlparse`'s parser wrapper knows nothing about lanes / git / output formats.
- The rule types know only the wrapper + extractor; nothing else imports
  go-pgquery.

## 4. `sqlextract` promotion (acceptance (b) + (c))

**[R4 — scope narrowed after review.]** `sqlextract` owns **only the hard part:
reassembling SQL held in Go string literals, and reporting what it could not
reassemble.** It does **not** own statement splitting: `splitStatements` and the
line-collapse glue in `goStatements` (`sqltext.go:198-206`, which forces every
statement from one candidate to the candidate's line) **stay in `sqltext`**.
This keeps the promotion mechanically behavior-preserving — the line-handling
that a naive move would have perturbed never leaves `sqltext` — and it is the
right boundary anyway, because the parse-tree rules do **not** pre-split (§5.1).

Move `extractGoCandidates` + `stringValue` into `internal/sqlextract/`, adding
the unresolved return:

```go
// Candidate is one maximal reassembled SQL-bearing Go string literal
// (a lone literal or a constant-folded `+` chain), plus the source line of the
// string expression it came from. NOT statement-split. Partial is true when the
// literal is a FRAGMENT of a composition that could not be reassembled (a
// fmt.Sprint* format string, or a '+' concat operand beside a non-literal) —
// it is not a standalone statement. [R9]
type Candidate struct { Text string; Line int; Partial bool }

// Site is a Go-literal SQL composition the extractor could NOT reassemble
// (fmt.Sprintf, strings.Builder, `+` with a non-literal operand, literals split
// across assignments) — today silently dropped. Carries position + reason.
type Site struct { Path string; Line int; Reason string }

func FromGo(path string, src []byte) (resolved []Candidate, unresolved []Site, err error)
```

- **`sqltext`** calls `FromGo`, ignores `unresolved` **and `Partial`**, and
  applies its **own** (unchanged, still-local) `splitStatements` + line-collapse
  to every `Candidate` — so all 19 consumer rules keep byte-identical verdicts
  (criterion c, proven by `sqltext`'s existing fixtures staying green). `Partial`
  is purely additive: `sqltext` has always collected these fragments (verified
  2026-07-28) and must keep doing so.
- **[R9] The parse-tree rules skip `Partial` candidates** (§5.2). A fragment like
  `"SELECT * FROM "` (from `"SELECT * FROM " + table`) is SQL-shaped but
  un-parseable; fed to `sql/parses` it would be a false positive. Marking it at
  the extractor — rather than dropping it from `resolved`, which would change
  `sqltext`'s input — keeps `sqltext` exact while hiding fragments from the
  parser. `Partial` is set by a post-process: a collected literal whose source
  position falls inside an unresolved node's span is a fragment. The walk that
  produces `resolved` (Text, Line, order) is otherwise unchanged.
- The `unresolved` return exists and is unit-tested at the package level: a
  `fmt.Sprintf`-composed query yields a `Site` rather than being dropped
  (criterion b).
- *Emitting* unresolved sites as findings is an **opt-in** that **no rule in
  this increment enables** — the consumer's `--inventory` census is out of
  scope, and a fleet-wide behavior change must not ride across on a refactor.

A `.go` parse failure remains a returned error (engine → exit 2), never a silent
pass — unchanged.

## 5. `internal/rules/sqlparse`

### 5.1 The go-pgquery wrapper

One place owns "SQL text → parse tree, or a parse error," wrapping
`pgquery.Parse` (→ PG17 protobuf AST) and the wazero runtime lifecycle. Both
rule types call it; nothing else touches go-pgquery.

**[R1 — the parser is the splitter, not us.]** The rules hand **whole** SQL text
to `pgquery.Parse`, which returns `ParseResult.Stmts []*RawStmt` — the correct,
grammar-aware statement split. Each `RawStmt.StmtLocation` (byte offset) maps to
a source line. The rules **never** pre-split on `;`; doing so would re-break on a
`;` inside a string literal or a dollar-quoted PL/pgSQL body — the exact
limitation §1.2 names.

Statement sources:

- **`.sql` file:** hand `f.Content()` whole to `Parse`; line = base + offset of
  each `RawStmt.StmtLocation`.
- **`.go` file:** call `sqlextract.FromGo`; hand **each `Candidate.Text` whole**
  to `Parse` (a candidate may itself contain multiple statements — the parser
  splits them); line = the candidate's line (interior offsets within a
  reassembled literal are not reliable, matching `sqltext`'s existing collapse).

Two engine-inherited constraints:

- **Concurrency.** `CheckFile` runs in the engine worker pool (`rules.Checker`:
  "must be safe for concurrent use"). go-pgquery builds its wazero runtime with a
  per-runtime compilation cache, so the wrapper builds **one** runtime at init
  and serves concurrent `Parse` calls from a guarded instance pool (verified /
  guarded at implementation time, not assumed) (§7).
- **Cost.** Parsing is in-process CPU, so `CostFast` (as `goast` is for Go). The
  wazero module compiles **once** for the reused runtime and amortizes over a
  whole-repo scan (the review confirmed the cache is per-runtime, not per-call).
  Acceptance still times a whole-repo run against the pre-commit lane budget; if
  it does not amortize, the types declare `CostHeavy` (§7).

Both rule types are per-file and monotonic (they do **not** implement
`WholeTreeInvariant`; restricting to a changeset can only remove findings).

### 5.2 `sql/parses`

The trivial-but-real check that proves the wiring end to end.

- **Params:** none (empty param struct → any field is a strict-decode error).
- **`.sql` files:** the whole file must parse; a parse failure is one finding at
  the failing statement's line.
- **`.go` files [R2 — SQL-shape gate; R9 — skip `Partial`].** The extractor
  returns *every* reassembled string literal, including import paths, struct
  tags, log/error strings, and **fragments of unresolvable queries** (`Partial`).
  The parse-tree source (§5.1) drops `Partial` candidates before parsing, so a
  `"SELECT * FROM "` concat fragment never reaches `sql/parses`. Of the
  remaining (complete) `.go` candidates, `sql/parses` parse-checks one
  **only if it is SQL-shaped**: after stripping leading whitespace/comments, its
  first keyword is a statement-initial SQL keyword (`SELECT`, `INSERT`, `UPDATE`,
  `DELETE`, `WITH`, `CREATE`, `ALTER`, `DROP`, `TRUNCATE`, `GRANT`, `REVOKE`,
  case-insensitive). This is a **named heuristic** — a deliberately conservative
  SQL-ness gate, fixtured so an `import "fmt"` and a `json:"..."` struct tag are
  **not** flagged while a malformed SQL-shaped literal **is**. Without it,
  `sql/parses` would emit hundreds of false positives on any real `.go` tree.
- Unresolved Go-literal sites are an extraction gap, not a parse failure —
  `parses` leaves them alone (§4 opt-in emission).

### 5.3 `sql/locking-select-order` (the flagship, acceptance (a))

```yaml
- id: locking-select-deterministic-order
  type: sql/locking-select-order
  severity: error
  scope:
    include: [freightworks/**/*.go]
    exclude: ['**/*_test.go']
  params:
    unique_key_columns: [id]         # default ["id"]; the exemption's boundary
    order_requires_unique_key: true  # default true; see criterion (1)
  cure: Order the locked rows (ORDER BY id) — two unordered same-set writers form a lock cycle.
```

**Selection [R7 — recurse].** Walk the whole statement tree and select **every
`SELECT` node carrying a non-empty `LockingClause`** (`FOR UPDATE` / `FOR NO KEY
UPDATE` / `FOR SHARE` / `FOR KEY SHARE`), including SELECTs nested in a CTE
(`WITH x AS (SELECT ... FOR UPDATE)`), a sublink, or an `INSERT ... SELECT ...
FOR UPDATE` target. Each selected node is evaluated with its **own** `FromClause`
/ `LockingClause`. Top-level-only selection would silently miss idiomatic
lock-and-return CTEs — a false negative on exactly the deadlock population the
rule exists to census.

A selected node is **compliant** iff any of:

0. **Every locking clause uses `SKIP LOCKED`** (`LockWaitPolicy == LockWaitSkip`)
   **[#41].** A `SKIP LOCKED` lock never waits on a row another transaction holds
   — it skips it — so it can never be the waiting edge of a lock-wait cycle and
   cannot deadlock, whatever its `ORDER BY`. Unconditional, and checked first.
   `NOWAIT` (`LockWaitError`) also cannot deadlock (it errors instead of waiting)
   but is **not** exempted here — its reasoning differs and is decided
   separately. A statement mixing a `SKIP LOCKED` clause with a blocking one is
   not exempt: the blocking clause can still wait.
1. **Its `ORDER BY` establishes a total order.** With
   `order_requires_unique_key: true` (default), the `SortClause` must include a
   column from `unique_key_columns` — a **heuristic** for "total order", because
   the parser cannot prove a column is unique. This is true to the goal: a
   non-total order (`ORDER BY status` over many locked rows) still deadlocks, so
   a bare `SortClause` is not enough. Set `order_requires_unique_key: false` to
   accept any `ORDER BY` (weaker; for legacy scopes) — the spec's §1 "deterministic
   ORDER BY" describes the default.
2. **It is a single-row unique-key lookup** — the exemption below.

Everything else is flagged (`cure:` = order the locked rows).

**The exemption is a heuristic, named as one.** The parse tree gives
`WHERE id = $1` and the range table to resolve aliases; it does **not** establish
that `id` is the primary key — a schema fact the parser cannot see. The
exemption holds iff **all** of:

- **Exactly one relation is locked [R3 — resolve the locked set].** Resolve it:
  `LockingClause.LockedRels` non-empty (`FOR UPDATE OF a`) ⇒ exactly those
  relations; `LockedRels` **empty** (bare `FOR UPDATE`) ⇒ **all base relations in
  `FromClause`**. More than one locked relation ⇒ not single-row ⇒ requires
  `ORDER BY`. (Keying on `len(LockedRels)` alone is wrong: a bare `FOR UPDATE`
  over a two-table join has zero `LockedRels` yet locks both.)
- The `WhereClause` binds a `unique_key_columns` column **of the locked
  relation** by **equality** — an `A_Expr` with **`Kind == AEXPR_OP`** and
  operator `"="` [R5]. `id = ANY($1)`, `id IN (...)`, `id BETWEEN ...` surface as
  an `A_Expr` with the **same** operator name `"="` differing only in `Kind`
  (`AEXPR_OP_ANY` / `AEXPR_IN` / `AEXPR_BETWEEN`); these lock many rows and are
  **not** exempt.
- The binding is **conjunctive** — no `OR` reachable from the top of the
  predicate (`BoolExpr` `OR`/`NOT` nesting breaks the exemption).

**Unparseable input is not this rule's concern.** A statement that fails to
parse has no tree to walk, so `sql/locking-select-order` silently skips it — the
rule ran fine; the input simply was not analyzable (not a rule crash). Surfacing
SQL syntax errors is `sql/parses`' job; configure the two together so a
malformed `.sql`/`.go` file is not left unchecked by both. The split is
deliberate and fail-safe for a deadlock rule (a syntax error blocks deployment
anyway), not a silent fail-open.

**Correction (#12 review): the pairing does not close the gap for *composed*
`.go` queries.** A syntax error in a query built by `+` concatenation or
`fmt.Sprintf` is reported by neither rule. `sql/locking-select-order` sources
via `sqlextract.FromGoReassembled` and drops parse failures; `sql/parses`
sources via `sqlextract.FromGo`, which marks every fragment of an unresolvable
composition `Partial` and skips it. Configuring both still leaves composed
`.go` SQL unchecked for syntax. This stays as-is rather than being closed: the
only text available to check is the placeholder reassembly, and a reassembled
fragment routinely fails to parse for reasons that say nothing about the real
query (`WHERE a IN (fw_expr)` is not a statement), so reporting those failures
would trade a silent gap for a stream of false positives on valid code.
Whole-literal `.go` queries and `.sql` files are unaffected — for those the
pairing is complete as described above.

## 6. Testing & acceptance (TDD-first, per the non-negotiable)

**[R8 — testing model corrected.]** Rule types in this repo are **Go
table-tested**, like `goast` (`goast_test.go`) and `sqltext` (`sqltext_test.go`)
— *not* self-hosted under `.formwork/`. `formwork lint`'s fixture-coverage keys
on each **configured rule `id`** over `cfg.Rules` (`internal/meta/lint.go`,
`internal/fixturetest/run.go`), so it does **not** force a fixture per registered
*type*; only the two self-hosted housekeeping rules are covered that way. There
is no `--skip-fixtures` flag in formwork (it is named only in deferred parity
notes, which are not part of this repo).

- **`sqlextract`** (Go unit): a `fmt.Sprintf`-composed query returns an
  `unresolved` Site rather than being dropped **(b)**; a **multi-statement Go
  literal pins the 2nd statement's line** (locks in the behavior-preservation
  the review flagged as untested); `sqltext`'s existing tests stay green **(c)**.
- **`sql/parses`** (Go table tests): a malformed statement fires; valid PG
  passes; a `.go` file whose only literals are `import "fmt"` + a `json:"..."`
  struct tag yields **no** finding (SQL-shape gate); a `;`-inside-a-string /
  dollar-quoted body parses clean (no naive-split false positive).
- **`sql/locking-select-order`** (Go table tests): unordered sibling-row locking
  `SELECT` fires; `WHERE id = $1 ... FOR UPDATE` single-row lock passes **(a)**;
  plus fixtures pinning each heuristic boundary — `id = ANY($1)` fires;
  `ORDER BY status` (non-unique) fires under the default; multi-table bare
  `FOR UPDATE` fires; `FOR UPDATE OF a` single-row passes; a CTE-nested
  `FOR UPDATE` is selected; `FOR UPDATE SKIP LOCKED` passes regardless of
  `ORDER BY` while the same query without `SKIP LOCKED` fires, and `NOWAIT`
  still fires **[#41]**.
- **Optional end-to-end:** an `examples/` fixture tree exercises the types
  through `formwork test` if desired (as the `palletra-port-*` examples do).
- **Self-host:** `make verify` green — test + vet + fmt + `make check` +
  `make selftest` + `make lint` **(d)**.

## 7. Risks carried into implementation (measure, do not assume)

1. **wazero compile cost vs. pre-commit budget.** The compilation cache is
   per-runtime, so build one runtime and reuse it; time a whole-repo
   `sql/parses` run against the pre-commit lane budget. If it does not amortize,
   declare `CostHeavy`. Default `CostFast`.
2. **go-pgquery concurrency safety under the worker pool.** Serve concurrent
   `Parse` from a guarded instance pool over the single runtime; verify safety.
3. **Untagged pseudo-version + embedded wasm binary size.** Pin the exact
   version in `go.mod`; record the binary-size bump as a deliberate cost.

## 8. Scope

**In:** the `sqlextract` promotion (Go-literal reassembly + `unresolved` return
+ unit test, emission opt-in and off), the go-pgquery wrapper, and the two rule
types `sql/parses` + `sql/locking-select-order`.

**Deferred to fast-follow PRs (separate `sql/*` types):** `no-star-select`,
`has-limit`, `unnest-update-ordered` (`locking-strength` has since landed
as `sql/locking-target`, #37).

**Out:** authoring the consumer's rule set; migrating any of
the 19 existing `sql/statement-predicate` rules; the `--inventory` census
wiring and unresolved-site *emission* (both tracked downstream); squawk integration
(`type: command` already covers it); the vendored-tree retirement
(in the parity dossier, kept with the validating target).

## 9. Evidence (perishable — re-derive at pickup)

- `sqltext.go` anchors (`params.table` `:62-64`, gate `:115`, `goStatements`
  line-collapse `:198-206`, `extractGoCandidates`/`stringValue` `:222-269`) read
  2026-07-28.
- `go/*` precedent: `internal/rules/goast/analyzers.go` — five `rules.Register`
  calls, one param struct each; Go-table-tested in `goast_test.go`.
- Testing/lint conventions: `internal/fixturetest/run.go` keys on `r.ID` over
  `cfg.Rules`; no `--skip-fixtures` in Go; only `.formwork/rules/housekeeping.yaml`
  self-hosted. Read 2026-07-28.
- `go-pgquery` `@latest` = `v0.0.0-20260728010200-155ebad2880e`; `go.mod`
  requires `wazero v1.12.0` + `pg_query_go/v6 v6.2.2`; wazero runtime uses a
  per-runtime `NewCompilationCache`.
- Design spec §14 WASM/pure-Go precedent: lines 485–486.

## 10. Adversarial-review revisions (summary)

Four-lens review (go-pgquery reality, code fidelity, heuristic soundness, scope),
each finding refuted against the real code before acceptance — 9 confirmed of 16.

- **[R1] blocker** — parse whole content; the parser splits (no naive `;`
  pre-split). §5.1.
- **[R2] blocker** — SQL-shape gate on `.go` candidates so `sql/parses` does not
  fire on imports/tags/log strings. §5.2.
- **[R3] major** — resolve the locked set from `FromClause` when `LockedRels` is
  empty; do not key on its length. §5.3.
- **[R4] major** — keep `splitStatements` + line-collapse in `sqltext`;
  `sqlextract` owns only reassembly, so the promotion can't shift consumer lines.
  §4.
- **[R5] major** — require `A_Expr.Kind == AEXPR_OP` for the equality exemption
  (reject `= ANY` / `IN` / `BETWEEN`). §5.3.
- **[R6] major** — `order_requires_unique_key` (default true): a non-total
  `ORDER BY` does not satisfy the deadlock goal. §5.3.
- **[R7] minor×2** — recurse selection into CTE / sublink / `INSERT…SELECT`
  locking SELECTs. §5.3.
- **[R8] minor** — corrected testing model (Go table tests; no `--skip-fixtures`;
  lint keys on rule `id`). §6.
- **[R9] critical (task-1 review)** — the extractor collects fragments of
  unresolvable queries (`"SELECT * FROM " + t` → `"SELECT * FROM "`), which
  `sqltext` has always tolerated but which would false-positive `sql/parses`.
  Mark them `Partial` (keeping `sqltext` byte-identical) and skip them on the
  parse-tree path. §4, §5.2.
