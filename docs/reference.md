# formwork — operator reference

The configuration surface, in full: every rule type and its parameters, every
preprocessor, the `formwork.yaml` schema, the exemption grammar, lane and
file-set semantics, and the exit-code contract.

This is the reference. [`docs/quickstart.md`](quickstart.md) is the narrative
introduction; read that first if you have not adopted formwork yet.

## How completeness is checked

The rule-type and preprocessor lists here are **checked against the binary's
registries, not against memory**. Two ways:

```sh
formwork list types            # every registered rule type
formwork list preprocessors    # every registered preprocessor
```

Both read the registry directly, so a pinned binary cannot misreport its own
vocabulary — which is why they, and not this document, are the authority.

Five assertions in `internal/meta` keep this document level with them, and it
is worth knowing exactly how far they reach, because "the docs are checked" is
a claim that is usually broader than the check behind it:

| assertion | what it fails on |
|---|---|
| `TestReferenceManualCoversEveryRegisteredType` | a registered rule type with no entry here |
| `TestReferenceManualCoversEveryRegisteredPreprocessor` | a registered preprocessor missing from the table below |
| `TestReferenceManualDocumentsEveryParameterOfEveryType` | an entry that omits a parameter the type's factory decodes — read from the struct tags, so a renamed parameter fails here rather than in your config |
| `TestReferenceManualGivesEveryTypeAWorkingExample` | an entry with no `yaml` example, or one `formwork` would refuse: every example below is written out as a rule file and loaded through the real strict decode |
| `TestReferenceManualStatesTheRegistryCountsCorrectly` | any count stated in this file that disagrees with the registry |

What they do NOT assert is the reverse direction — that everything documented
here is a registry entry — because this file legitimately documents things that
are not (the exit-code contract, the `formwork.yaml` schema, the marker
grammar), and enumerating those exceptions would be a list that itself rots.
Prose accuracy inside an entry is likewise not mechanically checkable; a
handful of facts too easy to get wrong are pinned individually.

Counts at the time of writing: **27 rule types, 14 preprocessors.** If your
binary reports different numbers, believe your binary and file an issue against
this document.

---

## The exit-code contract

The strongest guarantee formwork makes, and the one thing it would rather break
a release than get wrong.

| code | meaning |
|------|---------|
| `0` | pass — every selected rule ran and none found a violation |
| `1` | violations found |
| `2` | engine or configuration error — the run did not reach a verdict |

The distinction that matters is `1` versus `2`. A crashed, panicking, or
misconfigured rule must **never** read as a pass. Collapsing the two
(`formwork check || true`) discards the difference between "your repo is clean"
and "nothing was ever checked".

This contract does not change in 0.x. See
[VERSIONING.md](../VERSIONING.md).

---

## `formwork.yaml`

Lives at `.formwork/formwork.yaml`. Decoded **strictly**: an unknown field is
exit 2, not a warning. That is deliberate — a typo'd key that silently does
nothing is a guardrail you believe you have and do not.

```yaml
version: 1                      # required
engine: ">= 0.3.0"              # optional; refuse to run below this version
lanes:                          # optional; see Lanes
  ci:
    all: true
    ci: true
  pre-commit:
    tags: [fast]
    cost: fast
    ci: false
scope:                          # optional; classifier inputs for `formwork scope`
  docs: ["docs/**", "**/*.md"]
  governance: [".formwork/**"]
  languages:
    go: ["**/*.go"]
scan:                           # optional; prune the shared walk
  ignore:
    - glob: "vendor/**"
      reason: "third-party source, not ours to police"   # MANDATORY
  gitignore:
    reason: "build output is large and never committed"  # MANDATORY
```

### `engine:`

A semver constraint on the binary. Checked **before any rule file is parsed**,
so an incompatible engine refuses rather than evaluating with unknown
semantics. Declare it: it is half of your protection, the other half being a
pinned binary.

### `scan.ignore`

Doublestar globs pruned from the shared filesystem walk, hiding paths from
**every** rule at once. Each entry's `reason` is mandatory, and `formwork lint`
enumerates every entry with its **live match count** — so a typo'd glob reads
`0 matches` forever instead of silently protecting nothing.

`formwork lint` also fails on any **git-tracked** path a `scan.ignore` glob
hides: pruning is only sound while pruned paths stay uncommitted.

### `scan.gitignore`

Opt-in. Prunes what git reports **ignored** — never what it reports merely
untracked. The distinction is load-bearing: an untracked `.go` file is still
compiled by `go build ./...`, so skipping it would be a rule bypass, whereas
git will not take an ignored path into a commit without an `add -f`.

When git cannot answer, **nothing is pruned** and both `check` and `lint` say
so rather than reporting `0`.

---

## Rule files

Live at `.formwork/rules/*.yaml`. Also strictly decoded.

```yaml
rules:
  - id: no-todo-comments        # required; lowercase-hyphen
    type: forbidden-pattern     # required; see Rule types
    severity: error             # error | warn   (default error)
    preprocess: decomment-go    # optional; see Preprocessors
    scope:
      include: ["**/*.go"]
      exclude: ["**/*_test.go"]
      min_files: 5              # optional floor; see below
    params:                     # type-specific; strictly decoded
      pattern: "TODO"
    except:                     # optional; see Exemptions
      paths: ["legacy/**"]
      marker: true
      allowlist: allowlists/legacy.txt
    cure: "Delete the TODO or file an issue and link it."
    origin: "#412"              # what wound this rule closed
    tags: [fast, go]
    fixture_exempt: "..."       # heavy rules only; see below
```

### A note on quoting `cure` and `origin`

A **plain** (unquoted) YAML scalar ends at ` #`, and the rest becomes a
comment:

```yaml
cure: converted call sites consume the accessor (audit-1 #14)
```

reaches the engine as `converted call sites consume the accessor (audit-1`. The
`prose-not-truncated` lint check reports this. **Quote any prose containing
` #`.** A `#` with no whitespace before it (`issue#14`) is literal and safe.

### `scope`

`include` and `exclude` are doublestar globs, matched against repo-relative
paths. A file is in scope when it matches an `include`, no `exclude`, and no
`except.paths`.

`scope.min_files: N` (default 0 = off) turns a shrunken scope from a disclosure
into a verdict: fewer than N in-scope files is an error-severity, path-less
finding naming both numbers. It is a claim about the repository, never about a
changeset — but the set it is measured against differs by mode. Whole-tree
`check` counts the walk; `--staged`/`--range` counts the **tracked** tree,
because untracking a corpus that is still on disk otherwise passes a pre-commit
run and fails a fresh clone of the same commit.

### `severity`

`error` (exit 1 on a finding) or `warn` (reported, does not fail). Anything
else is exit 2.

### `fixture_exempt`

Heavy rules only (`command`, `git-diff`). A rule that cannot be driven by a
fixture tree declares **why**:

```yaml
fixture_exempt: "shells out to git; a fixture tree cannot reproduce the history"
```

Without it, a heavy rule carrying no fixtures is reported by
`fixture-coverage`. The exemption is not inferred from cost, because that made
"cannot be fixtured" and "nobody bothered" the same state. Declared exemptions
are enumerated by the escape-hatch census with their reason.

---

## Rule types

27 registered rule types. Parameters below are the strictly-decoded set — an unknown one is
exit 2. Each entry gives what the type asserts, every parameter it decodes, and
a worked example.

The examples are executable documentation, not illustration:
`TestReferenceManualCoversEveryRegisteredType` and its two companions in
`internal/meta` require every registered type to have an entry here, require
that entry to name every parameter the type's factory decodes, and hand the
entry's example to that factory. An example the engine would refuse, or a
parameter it no longer has, fails the build — which is what "this document
cannot fall behind the engine" has to mean to be worth writing down.

For the exact semantics of a rule as configured in *your* repo, ask the binary:
`formwork explain <rule-id>`.

### Declarative core

#### `forbidden-pattern`
Reports files matching a pattern.

| param | meaning |
|---|---|
| `pattern` | the regex to forbid |
| `all_of` | co-occurrence: violate only if EVERY pattern appears in the file |
| `none_of` | with `all_of`: and none of these appear |
| `require_present` | file-level guard on `pattern`: report only if the file contains ALL of these |
| `require_absent` | file-level guard: report only if the file contains NONE of these |
| `prefilter` | literal substring gate — a pure optimization (see below) |
| `syntax` | regex flavour |
| `multiline` | match across line boundaries |
| `window` | bounded multiline window |

**`prefilter` is a contract, not a hint.** It must not change any verdict.
`formwork lint`'s `prefilter-load-bearing` check evaluates the rule with and
without it and reports a disagreement — including a verdict of **unproven**
when it has no evidence either way, because a check that cannot fail is a check
that passes.

```yaml
- id: no-todo-comments
  type: forbidden-pattern
  severity: error
  scope:
    include: ["internal/**/*.go"]
  params:
    pattern: '//\s*(TODO|FIXME|HACK)\b'
  cure: "Resolve the item now or track it in an issue; marker comments rot."
```

#### `required-pattern`
Requires a pattern to be present. `pattern` is the regex, `syntax` its flavour
(as above), and `mode` chooses the unit: `every-file` (the default) reports
each in-scope file that lacks the pattern, `exists` reports once, at the end,
if no in-scope file carried it.

```yaml
- id: spec-exists
  type: required-pattern
  scope:
    include: ["docs/specs/*.md"]
  params:
    pattern: 'Formwork — Design Spec'
    mode: exists
  cure: "The design spec is the source of truth; do not remove it."
```

#### `pattern-count`
Asserts how many matching lines the whole scope holds. `pattern` is the regex,
`n` the count, and `op` one of `exactly`, `at-most`, `at-least`. The verdict is
scope-level: the tally runs across every in-scope file, so this says "the repo
has one of these", not "each file has one".

```yaml
- id: one-main-per-binary
  type: pattern-count
  scope:
    include: ["cmd/formwork/*.go"]
  params:
    pattern: '^func main\('
    op: exactly
    n: 1
```

#### `ordering`
Asserts one anchor precedes another within a file: the first line matching
`after` must not come before the first line matching `before`. A file missing
either anchor is not a finding. `within` takes `file` — the only unit — and may
be omitted.

```yaml
- id: license-header-precedes-package
  type: ordering
  scope:
    include: ["internal/**/*.go"]
  params:
    before: '^// SPDX-License-Identifier'
    after: '^package '
    within: file
```

#### `file-size`
Line-count caps. `cap` is the default ceiling, `hard_cap` an absolute one that
clamps every other, and `overrides` is a list of `glob`/`cap` pairs — the first
glob matching the path REPLACES the default cap for it, so an override widens
as readily as it tightens. `hard_cap` still clamps the result, which is what
makes it the one a per-path override cannot widen. A cap of `0` is unlimited.

```yaml
- id: file-size-vendor-cap
  type: file-size
  scope:
    include: ["internal/**/*.go"]
  params:
    hard_cap: 750
    cap: 500
    overrides:
      - glob: "internal/rules/goast/*.go"
        cap: 600
```

#### `file-naming`
Judges the path, not the content. `forbid_ext` is a list of extensions
(`.bak`), `require_match` a regex every in-scope path must match, and
`reserved` a list of globs a path must not match. At least one must be set.

```yaml
- id: migrations-are-timestamped
  type: file-naming
  scope:
    include: ["db/migrations/**"]
  params:
    require_match: '^db/migrations/[0-9]{14}_[a-z0-9_]+\.(?:up|down)\.sql$'
    forbid_ext: [".bak", ".orig"]
    reserved: ["db/migrations/**/tmp_*"]
```

#### `binary-content`
`forbid_binary` reports a file whose content is not text; `max_bytes` reports
one larger than that many bytes. At least one must be set.

```yaml
- id: no-binaries-in-source
  type: binary-content
  scope:
    include: ["internal/**"]
  params:
    forbid_binary: true
    max_bytes: 262144
```

#### `doc-path-exists`
Asserts every path named by `pattern` exists on disk. Uses `os.Stat`, not the
scanned file set, so it also catches a citation of something gitignored. The
pattern must have **exactly one** capturing group, and that group is the path
token; a pattern with none, or with two, is exit 2 rather than a rule that
quietly cites the wrong thing.

```yaml
- id: readme-links-resolve
  type: doc-path-exists
  scope:
    include: ["README.md", "docs/*.md"]
  params:
    pattern: '\]\(((?:docs|internal|cmd)/[A-Za-z0-9_./-]+)\)'
```

#### `pair-consistency`
"If the trigger appears, the companion must too."

| param | meaning |
|---|---|
| `trigger` | the pattern that creates the obligation |
| `requires` | the companion the obligation demands |
| `where` | the unit: `same-file` (default), `same-dir`, `same-func` |
| `also_present` | gate: the obligation applies only to units also matching this |
| `obligation` | `presence` (default) or `countable` |

`also_present` needs a unit holding a text span, so it is accepted for
`same-file` and `same-func` and refused for `same-dir`, whose unit is a
directory assembled across files. `obligation: countable` is refused with
`same-dir` for the same reason.

`where: same-dir` is a whole-tree invariant: the verdict waits for the whole
scan, not one file.

```yaml
- id: migrations-are-transactional
  type: pair-consistency
  scope:
    include: ["db/migrations/**/*.sql"]
  params:
    trigger: '(?i)\bALTER TABLE\b'
    requires: '(?i)\bBEGIN\b'
    where: same-file
  cure: "Wrap the migration in BEGIN; … COMMIT; so a failure part-way leaves the schema unchanged."
```

#### `set-relation`
Compares two derived sets. Sides `a` and `b` each take `files` (globs
selecting that side's members), `pattern` (the extractor), `group` (which
capture group is the value, default 1), `min_count` (the minimum cardinality
that side must reach, so a zero-evidence join cannot pass) and `preprocess` (a
per-side content transform, letting the two sides sit on different planes).
`relation` is `subset`, `equal` or `disjoint`.

```yaml
- id: every-rule-has-a-fixture
  type: set-relation
  scope:
    include: [".formwork/rules/*.yaml", "fixtures/**/*.yaml"]
  params:
    relation: subset
    a:
      files: [".formwork/rules/*.yaml"]
      pattern: '^\s*- id: ([a-z0-9-]+)'
      group: 1
      min_count: 1
    b:
      files: ["fixtures/**/*.yaml"]
      pattern: '^rule: ([a-z0-9-]+)'
      group: 1
```

#### `baseline`
Compares extracted values against a committed baseline file. `pattern` and
`group` extract the values, `baseline` is the repo-relative path to the
tracked list, and `mode` is `exact` (report both directions) or `shrink-only`
(removals permitted, additions not).

```yaml
- id: skipped-tests-only-shrink
  type: baseline
  scope:
    include: ["internal/**/*_test.go"]
  params:
    pattern: 't\.Skip\("([^"]+)"\)'
    group: 1
    baseline: .formwork/baselines/skipped-tests.txt
    mode: shrink-only
```

### Go analyzers (`internal/rules/goast`)

Real AST analysis, not regex approximation. Every pattern below is matched
against a NAME the parser produced — a function's name, a call's selector —
never against a line of source.

#### `go/func-line-budget`
Caps a function body's span. `max_lines` is required and counts the lines
between the body's braces; `funcs` is an optional regex confining the rule to
functions whose name matches.

```yaml
- id: factories-stay-readable
  type: go/func-line-budget
  scope:
    include: ["internal/rules/**/*.go"]
  params:
    max_lines: 80
    funcs: '^new[A-Z]'
```

#### `go/call-confined-to-func-name`
`symbol` may only be called from functions whose name matches `allowed_func`.
Both are required regexes: `symbol` matches the call's selector, `allowed_func`
the enclosing function's name.

```yaml
- id: exit-only-in-main
  type: go/call-confined-to-func-name
  scope:
    include: ["cmd/**/*.go"]
  params:
    symbol: '^os\.Exit$'
    allowed_func: '^main$'
```

#### `go/call-order-in-func`
Within each function matching `funcs`, the calls named by `sequence` must
appear in that order. `sequence` needs at least two entries — one is not an
order — and each is a regex over the call selector.

```yaml
- id: config-loads-before-flags-are-read
  type: go/call-order-in-func
  scope:
    include: ["cmd/**/*.go"]
  params:
    funcs: '^run$'
    sequence:
      - '^flag\.Parse$'
      - '^config\.Load$'
```

#### `go/guard-precedes-call`
Every `sink` call must be preceded, by source position within the same
function, by at least one `guard` call. `funcs` optionally confines the rule; a
call matching both patterns does not guard itself.

```yaml
- id: writes-are-permission-checked
  type: go/guard-precedes-call
  scope:
    include: ["internal/**/*.go"]
  params:
    guard: '^auth\.Require'
    sink: '^store\.Write'
    funcs: '^Handle'
```

#### `go/expected-derives-from-actual`
Flags a conformance test whose **expected** value is derived from the very
artifact it is checking. Such a test compares an artifact to itself: it can only
falsify the round trip — ordering, dedup, formatting — never agreement with
whatever the artifact is supposed to track.

| param | meaning |
|---|---|
| `reader` | regex over the selector of a whole-file read |
| `loader` | regex over the selector of a loader that returns parsed content |
| `compare` | regex over the comparison the test asserts on |
| `declare` | comment marker that declares a deliberate round trip |

```yaml
- id: conformance-tests-consult-the-writer
  type: go/expected-derives-from-actual
  scope:
    include: ["**/*_test.go"]
  params:
    reader:  '^os\.ReadFile$'
    loader:  '^(load|read)[A-Z]\w*$'
    compare: '^(bytes\.Equal|reflect\.DeepEqual|cmp\.Diff)$'
    declare: 'self-comparison:'
```

**This cannot be a pattern rule, and that is the whole point.** The correct form
and the defective form are the same TEXT. A golden-file test — read the
committed file, compare it to freshly rendered output — is the RIGHT shape, and
both spell `bytes.Equal(os.ReadFile(p), render(x))`. What separates them is
whether the expected side's data traces back to the same read, a local dataflow
fact with no textual signature. Measured on a live corpus, a grep for the shape
returned 20 candidates, nearly all correct golden tests.

**The trigger is the UNDECLARED self-comparison, not the shape.** A round-trip
normalisation check is legitimate and worth pinning — it is what keeps a large
generated file diffable and merge-friendly. So `declare` names a comment marker,
in the spirit of `# glob-dead:`, and a function carrying it is cleared. The cure
is to state the claim, not to delete the test.

Two properties of the dataflow are load-bearing:

1. **A call's function is not a data origin.** Only its ARGUMENTS carry data.
   Counting the callee would make every `filepath.Join` share an origin with
   every other and collapse the analysis.
2. **Function parameters are not origins**, and the exclusion comes from the
   signature rather than a name list. `*testing.T` reaches every helper in a
   body; counting a parameter as an origin makes two unrelated roots share one,
   and then every comparison in every test reads as a self-comparison.

A variable whose right-hand side yields no operand origins is its own root. Two
reads are the same artifact when the roots their arguments trace to overlap,
which is how a path built with `filepath.Join` from the same root is recognised
as the file a loader was pointed at.

#### `go/per-func-count-relation`
Counts two families of call per function and asserts a relation between the
tallies.

| param | meaning |
|---|---|
| `left` | regex over the call selector, counted on the left |
| `right` | regex over the call selector, counted on the right |
| `relation` | `<=`, `==` or `>=`, required |
| `funcs` | optional: confine the rule to functions whose name matches |
| `require_symbol` | `left`, `right` or `both`: fail if that side's pattern matched no call anywhere in scope — an anchor that stops a typo'd pattern reading as a satisfied relation |
| `require_used` | `left`, `right` or `both`: count a call on that side only when its result is used, so a discarded return does not satisfy the relation |

```yaml
- id: every-lock-is-released
  type: go/per-func-count-relation
  scope:
    include: ["internal/**/*.go"]
  params:
    left: '\.Lock$'
    right: '\.Unlock$'
    relation: '<='
    funcs: '^(?:Check|Finalize)'
    require_symbol: both
    require_used: right
```

### Dart analyzers (`internal/rules/dartscan`)

#### `dart/method-delegates`
A method matching `method` must call something matching `must_call`. Both are
required regexes; `method` matches the declaration line, `must_call` any line
of the body.

```yaml
- id: dispose-calls-super
  type: dart/method-delegates
  scope:
    include: ["lib/**/*.dart"]
  params:
    method: '^\s*void dispose\('
    must_call: 'super\.dispose\('
```

#### `dart/ref-after-await`
Flags a provider read after an `await` in the same method, where the widget may
already be gone. `method` (required) selects the methods to walk; `access` is
the read to catch, defaulting to `ref\.(read|watch)\b`; `guard` is the
liveness check that makes a read safe, defaulting to `mounted` and honoured
both on the read's own line and on an immediately preceding `if` line.

```yaml
- id: no-ref-after-await
  type: dart/ref-after-await
  scope:
    include: ["lib/**/*.dart"]
  params:
    method: '^\s*Future<[^>]*>\s+_?on[A-Z]'
    access: 'ref\.(read|watch)\b'
    guard: 'mounted'
```

#### `dart/numeric-field-validated`
A numeric text field must carry a validator. `numeric_value` (required)
identifies the field; `keyboard_arg` and `validator_arg` name the arguments to
look for, defaulting to `keyboardType` and `validator`; `validator_source` is
the shape a validator expression must have, defaulting to a
dotted-identifier-or-call chain so an inline closure is not mistaken for one.

```yaml
- id: quantity-fields-are-validated
  type: dart/numeric-field-validated
  scope:
    include: ["lib/**/*.dart"]
  params:
    numeric_value: 'TextInputType\.number'
    keyboard_arg: keyboardType
    validator_arg: validator
    validator_source: '^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)*(\(.*\))?$'
```

#### `dart/gate-reads-are-listened`
Flags a rebuild-scoped builder whose body decides something from a listenable
it was not given: the builder re-runs only for the listenables in its merge
list, so a decision read from anything else is served from a stale build.

| param | meaning |
|---|---|
| `builders` | the builder widgets to walk, by class name |
| `listen_args` | the arguments carrying the merge list — what the widget listens to |
| `builder_arg` | the argument holding the builder body |
| `read_suffixes` | the accessor suffixes that count as reading a listenable |
| `ignore_readers` | identifiers to exempt: read, but knowingly not gating |

```yaml
- id: gate-reads-are-listened
  type: dart/gate-reads-are-listened
  scope:
    include: ["lib/**/*.dart"]
  params:
    builders: ["AnimatedBuilder", "ListenableBuilder"]
    listen_args: ["animation", "listenable"]
    builder_arg: builder
    read_suffixes: [".text"]
    ignore_readers: ["_debugLabel"]
```

### SQL

Backed by the real PostgreSQL grammar (`libpg_query` compiled to WASM on
wazero — pure Go, no cgo). **The parser is the statement splitter**; there is
no naive `;` split, so a semicolon inside a string literal does not truncate a
statement.

`internal/sqlextract` reassembles SQL from Go string literals, so these rules
see queries composed in Go as well as `.sql` files.

#### `sql/parses`
Takes **no parameters**. Flags SQL that fails to parse. For `.sql` every
failure is reported; for `.go` only SQL-shaped candidates are, so an ordinary
string literal never fires.

```yaml
- id: sql-parses
  type: sql/parses
  scope:
    include: ["db/**/*.sql", "internal/**/*.go"]
```

#### `sql/locking-select-order`
A locking `SELECT` without a deterministic `ORDER BY` is a deadlock shape.
`unique_key_columns` lists the columns that make a lookup single-row (default
`[id]`), and `order_requires_unique_key` (default true) decides whether an
`ORDER BY` must end in one of them to count as a total order.

```yaml
- id: locking-selects-are-ordered
  type: sql/locking-select-order
  scope:
    include: ["internal/**/*.go", "db/**/*.sql"]
  params:
    unique_key_columns: [id, uuid]
    order_requires_unique_key: true
```

#### `sql/statement-predicate`
Per-statement assertions scoped to a table. `require` and `forbid` are lists of
regexes over the statement, and at least one of them is required.

`table` is an unanchored RE2 regex matched against the whole
**statement text**, so it selects statements rather than naming a relation: a
schema-qualified `app\.projects` works here, and matches any statement
mentioning it anywhere. `sql/locking-target`'s `table` is a different thing —
see its entry below.

```yaml
- id: deletes-name-the-tenant
  type: sql/statement-predicate
  scope:
    include: ["internal/**/*.go"]
  params:
    table: '(?i)\bDELETE\s+FROM\s+app\.orders\b'
    require: ['(?i)\bWHERE\b[^;]*\btenant_id\b']
    forbid: ['(?i)\bWHERE\s+true\b']
```

#### `sql/locking-target`
Which relation a locking clause locks, and how strongly, with `FOR UPDATE OF
<alias>` resolved against the statement's `FROM` bindings.

A different hazard from `sql/locking-select-order`, and neither covers the
other: ordering stops two writers taking the same rows in different sequences,
while an exclusive lock on a *specific* row deadlocks against a writer holding
a child row at a weaker strength however well ordered it is.

Alias resolution is why this is a typed rule and not a pattern. `FOR UPDATE OF
p` names an identifier, not a table, so a regex banning the two tokens'
co-occurrence fires on every statement that locks something *else* in a query
mentioning the guarded table.

`strength` is a **list** of the lock strengths to guard, drawn from `update`,
`no-key-update`, `share` and `key-share` — spelled as the SQL reads. `table`
and `strength` are both required: an empty or absent `strength` would match
nothing, which is a rule that cannot fire.

**`table` is an unanchored RE2 regex, matched against the relation NAME
alone** — not a table name, and not the statement text. Unanchored, so
`table: project` fires on the relation `projects`; write `^projects$` if you
mean exactly that one. Against the relation name alone, so a schema-qualified
value can never match, and is refused at exit 2 rather than accepted as a rule
that matches nothing:

```
table: app.projects     # config error — write the two below instead
schema: app
table: projects
```

Note that the same param name means something different one entry up:
`sql/statement-predicate`'s `table` matches the statement TEXT, where
`app\.projects` does work. The two are not interchangeable.

`schema` is an unanchored RE2 regex too, matched against the schema the
SOURCE qualifies the relation with. It is optional, and **an absent `schema`
guards every schema** — `other.projects` and `public.projects` both fire. It
does not default to `public`; there is no schema this rule declines to guard
until you name one.
Name it to tell same-named tables in different schemas apart; a finding always
names the relation the way the source wrote it, so you can see which one fired.

A relation the source does NOT qualify carries no schema at all: PostgreSQL
resolves a bare `projects` through `search_path` at execution time, which no
parse tree can know. Such a relation is reported under any `schema`, because
the guarded table is the likeliest thing it resolves to and a deadlock guard
that goes quiet on the ambiguous case misses the hazard it exists for.

```yaml
- id: no-exclusive-lock-on-orders
  type: sql/locking-target
  scope:
    include: ["internal/**/*.go", "db/**/*.sql"]
  params:
    schema: '^app$'
    table: '^orders$'
    strength: [update, no-key-update]
  cure: "Lock the parent row instead, or take FOR KEY SHARE: an exclusive lock here deadlocks against a writer holding a child row."
```

#### Known limits

The Go-literal fold does not model every control-flow construct. The
`COVERAGE LIMIT` block in `internal/rules/sqlparse/locking.go` is the current
list — it is maintained as a checkable claim, not as prose: every verdict in it
is run through these rules by the package's own tests, so one that stops being
true reddens the build.

One entry reaches you through the finding itself rather than through that
block, so it is written out here in full. Its machine name is
`closure-name-escape`, and the finding names #337 when it fires.

```go
package db

func run(f func()) { f() }

func load() string {
	q := "SELECT id FROM t WHERE s = 'x'"
	add := func() { q += " ORDER BY id" }
	run(add)
	q += " FOR UPDATE"
	return q
}
```

`sql/locking-select-order` reports an unordered locking `SELECT` on that query.
The closure appends the `ORDER BY` and `run` invokes it, so every real path
orders the query — but this rule reads one file and never resolves a callee, so
the value it built has that append missing.

**#337 — DECIDED:** the behaviour is kept, and the measurement is the reason.
Change one identifier:

```go
package db

func register(f func()) {}

func load() string {
	q := "SELECT id FROM t WHERE s = 'x'"
	add := func() { q += " ORDER BY id" }
	register(add)
	q += " FOR UPDATE"
	return q
}
```

To a parse-only pass those two programs are the same text: `run(add)` and
`register(add)` both measure **1 finding** at `unique_key_columns: [id]`. In the
second one nothing ever calls the closure, `SELECT … FOR UPDATE` with no
`ORDER BY` is the value that really executes, and
the finding is the deadlock hazard this rule exists for. Untracking a query
because a closure's name escaped deletes that finding in order to delete the
other one, so both are reported.

What the rule does instead is hand you the question to ask. The finding carries
a NOTE naming the escape as written and the callee it was handed to. That NOTE
is a decision procedure and never a verdict — it says the same thing about both
programs above, because to this rule they are one program. Read the callee it
names:

- If it **CALLS** the closure, every real path orders this query and the
  finding reports a hazard the code does not have. Clear it with a
  `formwork:allow` marker, which `formwork lint` keeps enumerated so the
  suppression stays visible; do not reshape the query to please the analyzer.
- If it only **STORES** the closure, nothing runs those appends, the unordered
  locking `SELECT` is the real value, and the finding is the deadlock hazard.

This shape over-reports and does nothing else. With the lock inside the closure
rather than outside it, the rule is silent for every call spelling — a separate
entry in the block above, and one with no marker to reach for.

### Heavy escape hatches

Both shell out, both are `cost: heavy`, and both are enumerated by the
escape-hatch census. These are the rules most worth reviewing.

#### `command`
Runs an external program.

| param | meaning |
|---|---|
| `cmd` | argv, run with the scan root as its working directory |
| `when` | arming condition; its one key is `paths_changed`, a non-empty glob list, and the rule runs only when a matching in-scope file is in the changeset |
| `expect` | the expected outcome: `exit` (the exit code to accept, default 0) and `output_forbid` (a regex whose match in the output is a violation) |

`formwork lint`'s `command-trigger-armable` check reports a `when.paths_changed`
that cannot intersect the rule's own `scope` — a gate that can never fire, in
any mode, on any commit.

```yaml
- id: dart-analyzes-clean
  type: command
  scope:
    include: ["lib/**/*.dart", "test/**/*.dart"]
  params:
    cmd: ["dart", "analyze", "--fatal-infos"]
    when:
      paths_changed: ["lib/**/*.dart", "test/**/*.dart"]
    expect:
      exit: 0
      output_forbid: '(?i)\bwarning\b'
```

#### `git-diff`
Judges a diff rather than a tree. `range` is required and is passed to git as
written; `forbid_added` and `forbid_removed` are regexes over the CONTENT of a
changed line — the leading `+`/`-` is stripped before matching, so anchor with
`^` for the start of the line itself. At least one of the two is required.

This rule does not consume the scan, but `scope.include` is still required of
every rule, so give it one: an empty scope is a config error, not a rule that
runs over everything.

```yaml
- id: no-new-vendored-binaries
  type: git-diff
  scope:
    include: ["**"]
  params:
    range: origin/main..HEAD
    forbid_added: '^\s*//go:embed .*\.(?:so|dylib|dll)$'
    forbid_removed: '^\s*func TestExitCodeContract\('
```

---

## Preprocessors

14 registered preprocessors. Declared per rule as `preprocess:`, computed once
per file and cached, so several rules sharing a variant pay for it once.

| name | what the rule sees |
|---|---|
| `raw` | the file unchanged (default) |
| `decomment-go` | Go source with comments removed |
| `strings-only-go` | only Go string-literal contents |
| `decomment-destring-go` | Go with both comments and string contents removed |
| `comments-only-go` | only Go comments |
| `decomment-sh` | shell with comments removed |
| `destring-sh` | shell with string contents removed |
| `destring-decomment-sh` | shell with both removed |
| `strings-only-sh` | only shell string contents |
| `code-only-dart` | Dart with comments and string contents removed |
| `comments-only-dart` | only Dart comments |
| `comments-only-sql` | only SQL comments |
| `comments-only-awk` | only awk comments |
| `qualify-proto-go-alias` | proto with top-level `message Name {` rewritten to `message alias.Name {` using the `go_package` `;alias` |

Choosing one is usually about **where a false positive comes from**. A rule
banning a token in code wants `decomment-go`, so a comment mentioning the token
does not trip it. A rule about what comments claim wants `comments-only-go`.

Note the direction: a `*-only-*` variant blanks everything else, so a rule
reading inside a Dart string literal cannot fire under `code-only-dart`.

---

## Exemptions

Three channels. All three are enumerated by `formwork lint`'s escape-hatch
census, and none of them is silent.

### Inline markers

Enabled per rule with `except: {marker: true}`.

```
formwork:allow <rule-id> <reason>
```

The id is a single whitespace-delimited token; everything after it is the
reason. `formwork lint`'s `exemption-hygiene` check reports a marker with no
reason.

Suppressed findings are **enumerated, not counted** — every renderer names
which findings were suppressed and by which channel, so the exemption surface
can be audited:

```
suppressed (exempted, not failures):
  [no-todo] a.go:2: forbidden pattern matched: TODO (marker)
```

### Allowlist files

`except: {allowlist: allowlists/legacy.txt}`, relative to `.formwork/`. One
repo-relative path per line. Ratchets: entries are meant to be removed, and
`exemption-hygiene` reports an entry that no longer trips the rule.

### `except.paths`

Globs carved out of the rule's scope. Distinct from the other two: it is a
scope **subtraction**, so the rule never evaluates the file and there is no
finding to suppress. The census reports how many in-scope files each entry
actually **removed**, so a dead entry reads `0 file(s)` rather than printing
like a live one.

---

## Lanes and file-set modes

### Lanes

Named selectors over rules, declared in `formwork.yaml`:

```yaml
lanes:
  pre-commit:
    tags: [fast]      # select by rule tag
    cost: fast        # fast | heavy
    ci: false
  ci:
    all: true
    ci: true
```

`formwork check --lane <name>` runs only that lane. `formwork lint` reports a
lane selecting **zero** rules, and a rule reachable by no CI lane.

### File-set modes

| invocation | what is scanned |
|---|---|
| `formwork check` | the whole tree |
| `formwork check --staged` | the staged changeset |
| `formwork check --range A..B` | the changeset in that range |

Both changeset modes intersect git's answer with the scan, and a path git names
that the scan did not produce is **refused** rather than skipped. Whole-tree
invariant rules (`where: same-dir`, `set-relation`, `baseline`) still evaluate
over the tracked tree in changeset modes, because a verdict about the
repository cannot be reached from a diff.

`--range` is **one string, tokenized shell-style** — not a list of arguments.
It carries the revisions and, optionally, a `-- <pathspec>` tail, and formwork
splits it into git arguments on unquoted whitespace, honouring `'…'`, `"…"` and
a backslash escape.

**A pathspec containing a space must therefore be quoted.** Unquoted, it splits
into two pathspecs, git matches neither and exits 0 with an empty set — every
per-file rule then sees zero files and the gate passes over an unscanned
changeset. Quoting is the resolution rather than refusal because a tail of
several space-free pathspecs is a supported shape, and is indistinguishable
from one spaced name until the operator quotes it. An unclosed quote or a
dangling escape is **exit 2**, never a guess at what was meant.

```sh
formwork check --range "origin/main..HEAD -- 'docs/design notes'"
```

What this does not make loud is a pathspec that matches nothing: that is git's
honest answer to a legitimate question — scoping a range to a subdirectory that
did not change — and there is no universe to test an arbitrary range's pathspec
against.

`formwork scope` selects a changeset with the same two flags and no
whole-tree mode; its modes are tabled under
[Introspection](#formwork-scope-file-set-modes).

---

## Introspection

Four commands answer questions **about** a configuration instead of judging a
tree: `list` and `explain` enumerate what is declared, `rules-for` says what
governs a path before it is edited, and `scope` classifies a file set for a CI
router.

Every one takes `-format human|json`, and the flag goes **before** the
operands — Go's flag parsing stops at the first positional argument, so
`formwork list -format json rules` is the working spelling and `formwork list
rules -format json` is exit 2. An unknown id, kind, format, or an out-of-root
path is exit 2 — an empty answer to a wrong question would be a guidance
fail-open.

```sh
formwork list [-format human|json] rules|lanes|types|preprocessors  # enumerate
formwork explain [-format human|json] <rule-id>                     # one rule in full
formwork rules-for [-format human|json] <path>...                   # what governs these paths
formwork scope [-format human|json] <path>...                       # docs | governance | runtime
formwork scope [-format human|json] [--staged | --range A..B]       # the same, for a changeset
```

`list types` and `list preprocessors` read the registries, so a pinned binary
cannot misreport its own vocabulary — which is why they, and not this document,
are the authority.

`TestReferenceManualIntrospectionCommandsTakeTheFormatFlag` (in `internal/meta`)
runs each of these lines against the binary, so the two promises above are
executable rather than asserted.

### `formwork scope` file-set modes

Two questions, and the operand decides which. Given paths, scope classifies
**those paths** — the form a CI router uses for a file it already knows changed.
Given none, it classifies a **changeset**, selected the way `check` selects one:

| invocation | what is classified |
|---|---|
| `formwork scope <path>...` | the named paths (which need not exist yet) |
| `formwork scope` | the staged changeset |
| `formwork scope --staged` | the staged changeset |
| `formwork scope --range A..B` | the changeset in that range |

There is no whole-tree mode: scope classifies a change, and a repository is not
a change. The two questions are mutually exclusive — a path operand together
with `--staged` or `--range` is exit 2, and so is a flag written *after* a path,
because flag parsing has already stopped there and the flag would otherwise be
discarded silently.

Output is one `class=` line plus one `<language>_changed=` line per declared
language, or the same under `class` and `languages` in JSON:

```sh
$ formwork scope docs/adr/0001.md
class=docs
go_changed=false
dart_changed=false

$ formwork scope -format json --range origin/main..HEAD
{
  "class": "runtime",
  "languages": { "dart": false, "go": true }
}
```

**scope is fail-closed, and says which answers were assumed.** In the changeset
modes a git error or an empty changeset yields `class=runtime` with every
language flagged changed, at exit 0 — the classifier informs gating, it does not
itself gate, and the costs are not symmetric: a wasted runtime lane is minutes,
a skipped one is a hole. That is an **assumed** classification rather than a
computed one, and it is announced on stderr and carried in JSON as an `assumed`
key, absent when the class was computed. A router that cannot tell the two apart
will eventually skip a lane it owed. Path operands are never assumed: they name
their own file set, so there is no git call to fail and nothing to assume
around.

---

## Self-testing, hooks, and version

These act on a repository rather than answering questions about it, and none of
them takes `-format`. `formwork check` is the command with a machine format
(`-format human|json|github`); its exit codes are the contract at the top of
this page.

```sh
formwork test                    # every rule against its fixtures
formwork lint                    # self-integrity checks
formwork hooks install|verify    # git hook shims
formwork version                 # the stamped release version
```

`test` and `lint` both take `--rule <id>`, which runs one rule's fixtures or one
rule's checks and is fail-closed on an unknown id. `hooks install` refuses
rather than taking over hook wiring that is already there; `hooks verify` judges
the installed shims and writes nothing.
