# Formwork — Design Spec

Date: 2026-07-09
Status: Approved design, pre-implementation
Validating target: a private production monorepo, ~251 shell guardrail scripts

## 1. Summary

Formwork is a single Go binary that evaluates repository guardrail rules defined
in tracked YAML configuration. It replaces per-repo fleets of shell "gate"
scripts (grep/awk/perl checks wired into git hooks and CI) with one engine, one
rule manifest, one fixture-driven test discipline, and one orchestration layer.

It is a **generic, repo-agnostic product**. That target's 251-script lockdown
system is the first configuration and the parity benchmark: v1 is done when all
251 rules run under formwork with equivalent firing behavior, validated against
the existing synthetic-violation fixtures.

### Goals

- All guardrail rules declared in reviewable YAML tracked in the adopting repo.
- One shared-scan engine: whole-tree evaluation of all fast rules in seconds
  (the current shell system spends its ~30s pre-commit budget on 190 processes
  that each re-walk the tree).
- Rule self-testing (`formwork test`) and system self-integrity
  (`formwork lint`) as built-in features, not bolt-ons.
- Full replacement of hook/lane orchestration (pre-commit, pre-merge-commit,
  pre-push, CI) including the docs/governance/runtime scope classifier.
- Portability: single static binary; erase the BSD-grep/bash-3.2/Windows-ulimit
  hazards the shell system fights.

### Non-goals

- **Tamper protection.** A binary cannot protect itself from the repo it runs
  in. Platform-layer protections (CODEOWNERS, `pull_request_target` approval
  workflows, branch protection) stay; formwork only *verifies* their wiring.
- **Replacing toolchain linters.** gofmt, staticcheck, `flutter analyze`, buf,
  and docker-based migration replays remain external tools; formwork invokes
  and interprets them via `command` rules.
- **Auto-fix.** V1 reports violations with cure text; it does not rewrite code.
- **Plugin protocol.** An external-check JSON protocol is a possible v2
  extension point; v1 covers everything with built-in types.

## 2. Context: what the corpus survey found

A full read of all ~251 `scripts/check-*.sh` gates plus the orchestration and
CI layers (2026-07-09) established that the scripts reduce to a small set of
mechanical archetypes:

| Archetype | ~Count | Share |
|---|---|---|
| forbidden-pattern (regex must not appear in a scope) | 80 | 32% |
| structural (stateful parsing: awk brace-walkers, perl body parsers, SQL reassembly) | 55 | 22% |
| pair-consistency (if X appears, Y must too; often cross-file) | 37 | 15% |
| required-pattern (anchors that must be present) | 27 | 11% |
| count-constraint (exactly-one / at-most-N) | 19 | 8% |
| external-tool (gofmt/staticcheck/flutter/buf/docker) | 15 | 6% |
| snapshot/baseline comparison | 9 | |
| diff/git-based | 6 | |
| file-size / naming / binary-magic | 6 | |
| meta (gates checking the gate system) | 6 | |

Cross-cutting machinery shared by nearly all scripts: scan-scope assembly
(`frontend/lib` + `packages/*/lib`), test/generated-file excludes,
comment-stripping before matching (shared awk lexers: `decomment-go.awk`,
`strings-only-go.awk`, `destring-sh.awk`), multiline/PCRE2 matching, and a rich
exemption taxonomy (canonical-file carve-outs, path allowlists, inline
justification markers, external allowlist files, build tags). House style
favors path/structural allowlists over per-line opt-outs.

The structural 22% falls into five families:

1. Function-scoped ordering/coupling in Go (guard-precedes-write with argument
   binding, auth-before-decode, log-before-500, per-handler count relations).
2. SQL predicates over strings reassembled from Go concatenation /
   `strings.Builder`.
3. Dart/Flutter lifecycle rules (ref-after-await, keepalive flush, pagination
   delegation).
4. Cross-file derived sets (extract set from A, assert relation with set from B).
5. Config parsers (workflow jobs, golangci config, pubspec dep graphs).

Self-integrity layer: ~100+ Go "synth" tests (`//go:build lockdown`) prove each
gate fires on a known-bad fixture; meta-gates enforce that every script is
wired into CI/hooks, has a firing synth, and that class-locking gates prove ≥2
violation forms. The gate list is enumerated three separate ways that must
agree (inline pre-commit list, ~250 explicit CI YAML steps, glob meta-gate).

Notable precedent: that corpus itself moved one gate's receiver-type check from
shell to a Go AST test after concluding lexical checking could not express it.

Output/exit conventions the ecosystem depends on: `[name] OK/FAIL — message`
verdict lines, `Cure:` remediation blocks, exit 0 = pass / 1 = violation /
2 = engine or usage error.

## 3. Decisions

1. **Product scope**: generic engine; the target corpus is the first,
   validating configuration.
2. **V1 surface**: rule engine + check runner, `formwork test`, orchestration
   (lanes/scope/hooks), and self-integrity meta-checks. Delivered in phases.
3. **Structural rules**: hybrid — named built-in analyzers implemented in Go
   (go/ast for Go; a lightweight lexer for Dart; SQL text reassembly), plus a
   tracked `command` rule type reserved for toolchain invocations. External
   check protocols and tree-sitter query rules are explicitly deferred.
4. **Definition of done**: full 251-rule parity, measured by a parity harness
   that runs original script and formwork rule against the same fixtures.
5. **Architecture**: typed rule registry over a two-phase shared-scan executor.

## 4. Architecture

Go module: `github.com/buildfoundry/formwork`. Single CLI binary `formwork`.
Go ≥ 1.24 (pin exact toolchain at scaffold time).

```
formwork/
  cmd/formwork/            CLI entry point (subcommands: check, test, lint,
                           scope, hooks, explain, list, rules-for, version;
                           init is designed in §10 but deferred — not built,
                           deliberately, until the open-source quickstart
                           arc (#110) decides its shape)
  internal/
    config/                YAML loading, strict schema validation, defaults
    scan/                  one-pass filesystem walk, file content cache
    engine/                planner + parallel two-phase executor
    rules/                 rule-type registry
      pattern/             forbidden-pattern, required-pattern, pattern-count,
                           ordering
      relation/            pair-consistency, set-relation
      goast/               go/ast built-ins
      dartscan/            Dart lexer built-ins
      sqltext/             SQL reassembly + statement predicates
      command/             external command escape (toolchain gates)
      fsrules/             file-size, file-naming, binary-content
      gitrules/            diff/commit-range rules
    preprocess/            Go ports of the awk lexers (decomment-go,
                           strings-only-go, destring-sh,
                           destring-decomment-sh), unit-tested
    exempt/                exemption taxonomy evaluation + staleness detection
    report/                human / JSON / GitHub-annotation renderers
    fixturetest/           `formwork test` runner (want-marker assertions)
    meta/                  `formwork lint` self-integrity checks
  tools/parity/            parity harness (phase 6) — DESIGNED, NEVER BUILT
  docs/
```

Each rule type implements a common interface and registers itself; the config
layer maps `type:` strings to implementations and validates type-specific
params with strict decoding (unknown YAML fields are errors, exit 2).

### Unit boundaries

- `scan` knows nothing about rules; it produces a file set with lazily-computed,
  cached content variants (raw, decommented, strings-only, …).
- Rule types know nothing about lanes, git, or output formats; they consume
  scoped file content / extractions and emit findings.
- `engine` owns planning (lane → rule set → scope union) and scheduling.
- `report` owns all formatting; findings are plain data.

## 5. Configuration model

Config lives in the adopting repo:

```
.formwork/
  formwork.yaml            engine settings, lanes, scope classes, defaults, library packs
  rules/*.yaml             rules grouped by domain (go-db.yaml, flutter-state.yaml, …)
  fixtures/<rule-id>/      fire-*/ and pass-*/ fixture trees
  allowlists/*.txt         external allowlist files (one entry per line, # comments)
```

`formwork.yaml` carries `version: 1` (config schema version) and a minimum
engine version constraint checked at runtime.

It may also carry `library: [generic]` to opt into rule packs embedded in the
binary (spec `docs/specs/2026-09-02-stdlib-library.md`). Pack rules load first;
a local file restating the same `id` replaces the pack rule. Unknown pack names
are a config error. This is how a second repo obtains the generic hygiene gates
without copy-paste.

It may also carry a `scan:` block:

```yaml
scan:
  ignore:
    - glob: '.claude/worktrees/**'
      reason: agent harness worktrees are checkouts of foreign branches
```

`scan.ignore` prunes matching paths from the shared walk before any rule sees
them — the one repo-global exclusion channel, intended for trees that are
present on disk but not this repo's code (nested checkouts, vendored source,
generated output). Globs are doublestar, repo-root-relative (leading `/` or
`./` rejected); `reason` is mandatory and enforced by strict decoding (exit 2
without it), because this channel weakens every rule at once and therefore
carries its justification in the schema, not in an optional lint check. A glob
that matches a *directory's* path prunes that entire subtree, including
descendants the glob would not match as files — a file-shaped glob like
`**/*.gen.go` therefore hides everything under a directory that happens to
carry a matching name. The census's `N dirs pruned` count is where that
widening shows.
The pruning is sound only while pruned paths stay uncommitted, and that is
checked, not assumed: `formwork lint` fails any git-tracked path under a
pruned glob (`scan-ignore-tracked`, §9).
A config using `scan:` should declare `engine: ">= <first release with it>"` so
an older binary fails legibly at the version gate rather than on an unknown
key. Ignores do not apply to fixture walks (fixture trees are their own path
namespace) and do not affect `formwork scope` (which classifies git-changed
paths and must stay fail-closed). `command`/`git-diff` rules shell out and can
still see ignored trees — the same asymmetry `--staged` already documents.

The block may also carry `scan.gitignore`:

```yaml
scan:
  gitignore:
    reason: git already refuses these, so no rule can be evaded by not reading them
```

`scan.gitignore` prunes every path git reports as **ignored**. It is opt-in and
absent means off, so a repo that does not declare it walks exactly as before.
`reason` is mandatory for the same reason `scan.ignore` requires one. There is
no boolean: a `prune: false` spelling would add a declared-but-inert state that
reads at a glance like the channel is live, and deleting the block is the
unambiguous way to turn it off.

**It prunes the ignored set, never the untracked set**, and the distinction is
the whole design. An untracked `.go` is still compiled by the toolchain and is
usually work in progress, so skipping it would be a rule bypass on the file
most likely to matter — the same lesson as the symlink refusal (§11). An
ignored path is different in kind: git will not accept it into a commit without
an edit to `.gitignore` or an explicit `add -f`. So an untracked-but-not-ignored
file stays fully in scope, and a **tracked** file cannot be pruned at all —
that last guarantee is git's own, because `git check-ignore` reports a tracked
path as not-ignored unless `--no-index` is passed, and the engine never passes
it.

`.gitignore` lives outside `.formwork/` and outside any governance scope, so
this channel could have been the one exclusion nobody audits. It is not: the
escape-hatch census names it on every `lint` run with live dir/file counts and
its reason (§9), and each prune record carries the `<file>:<line>:<pattern>`
that caused it.

**Degradation is fail-closed and reported.** When the key is declared but git
cannot answer — no repository, no git binary, a broken index — **nothing** is
pruned and the run says so (`check` on stderr, `lint` in the census as *"could
not determine"*, never as `0 matches`). Pruning nothing yields a scan that is a
strict superset of the declared one, so no rule can pass that would otherwise
have failed; that is why an unreadable index is a warning here rather than the
exit 2 the `--staged`/`--range` seam owes, where the fallback would *narrow*
what is gated. Like `scan.ignore`, it does not apply to fixture walks and does
not affect `formwork scope`.

### Rule envelope (shared by all types)

```yaml
- id: single-pool-constructor          # unique, kebab-case
  type: forbidden-pattern              # selects the implementation
  severity: error                      # error | warn
  origin: scripts/check-single-pool-constructor.sh   # optional traceability
  tags: [go, db]                       # lane/report selectors
  scope:
    include: ["freightworks/**/*.go"]
    exclude: [go-tests]                # named presets: go-tests, dart-tests,
                                       # generated, vendor; plus raw globs
    min_files: 0                       # optional floor: fail if the scope
                                       # selects fewer than N files (0 = off)
  preprocess: decomment-go             # raw (default) | decomment-go |
                                       # strings-only-go | destring-sh |
                                       # destring-decomment-sh
  params:                              # type-specific, strictly validated
    pattern: 'pgxpool\.New(WithConfig)?\('
  except:
    paths: ["**/internal/db/**"]       # path/glob carve-outs
    marker: true                       # honor inline `// formwork:allow <id> <reason>`
    allowlist: allowlists/pool-sites.txt   # external file; stale entries flagged
  cure: "Construct pools only via internal/db (db.WithTenant)."
```

Fixtures are located by convention (`.formwork/fixtures/<rule-id>/`), not
declared in YAML.

**`scope.min_files`** (#23) is the arming end of the empty-scope disclosure §9
describes. A rule whose scope selects nothing is *reported* and still passes,
because an empty scope is legitimate — a rule scoped to a path the repo has not
created yet is not a defect. `min_files: N` lets a repo whose port is finished
say the corpus is real: `check` emits an error-severity, scope-level finding
(exit 1) naming both numbers when the scope selects fewer than N files. The
default is 0 — no floor, exactly the behaviour that predates the key — so it is
opt-in per rule, following `set-relation`'s `min_count`. A negative or
non-integer value is a config error (exit 2) at load; `min_files` is decoded
from the raw YAML node rather than into an `int` because yaml.v3 *coerces*
there, reading `1.5` as 1.

It is never measured against the changeset — a floor is a claim about the
repository, so judging it on the changed files would false-fail every armed rule
on every commit, and skipping it in a file-set mode would let the pre-commit shim
pass what CI fails. What it *is* measured against differs by mode, and the
difference is load-bearing:

- **Whole-tree `check`**: the walk, untracked files included. That is exactly the
  set the engine scanned, and restricting it would make `check` require git in a
  tree that has none.
- **`--staged` / `--range`**: the **tracked** tree, the same set the whole-tree
  invariants above take, and for the mirror-image reason. An untracked file must
  not false-fail a pre-commit invariant, and it must not *satisfy* a floor
  either: untracking a corpus (`git rm --cached`, a `.gitignore` entry, files
  still on disk) is the commonest way a corpus vanishes, and counting the walk
  let exactly that commit pass the pre-commit shim while failing a fresh clone of
  the identical commit. The branch already lists the tracked set for the
  invariants, so this costs no new git dependency.

Four things do **not** evaluate a floor, and none of them is coverage: rules a
`--lane` filter did not select (selection working as asked); rules
`--skip-escapes` dropped, which say so on the drop line that already names them,
because emitting a finding for a rule the renderer no longer holds would exit 1
printing nothing; `formwork test`, whose fixture trees are small by construction;
and `formwork lint`, whose `empty-scope` check is the unarmed sibling of this
floor and judges only the zero case — so a rule armed at 5 and matching 2 passes
`lint` and fails `check`.

`formwork explain` renders an armed floor; an unset one is not
rendered, and it is **not** an escape-hatch census line (§9) — an armed floor
tightens a rule rather than exempting anything from it, and the unset default
would otherwise print one "no floor" line per rule for the whole corpus.

Exemption semantics (phase 3a): `marker: true` honors `formwork:allow
<rule-id> <reason>` on the violating line itself — same line only, and the
reason is mandatory (a reasonless marker never exempts; lint flags it). The
reason must contain at least one alphanumeric character once trailing
whitespace and a trailing comment-closer token (`*/`, `-->`, `#>`) are
stripped — a bare comment closer left over from `/* formwork:allow <id> */`
or `<!-- formwork:allow <id> -->` doesn't count as a reason. The engine and
`formwork lint` share one grammar for this (`internal/marker`), so they can
never disagree about what counts as a valid marker.
Marker scanning always reads the raw file, not the rule's preprocess variant.
Allowlist files hold exact repo-relative paths (no globs; one per line, `#`
comments allowed), resolve relative to `.formwork/`, and a missing file is a
config error. An entry is matched to a finding by its exact path FIRST, and only
where that fails by asking the filesystem whether the entry and the walked path
are the same file — `scan.(*FileSet).Produced`, device and inode, gated on a
non-ASCII byte on both sides, the same oracle `Restrict` folds NFC/NFD spellings
with. That fold is not glob matching under another name: it can move no ASCII
verdict, and it exists because on a normalization-insensitive filesystem an
entry an editor saved NFC exempted NOTHING against the NFD directory entry
readdir returns, silently, since an unsuppressed finding is indistinguishable
from one nobody exempted (#308). Exact stays first because a cross-file
finalizer may report a path the walk never produced, and only the exact test can
answer for one of those. Exempted findings are *suppressed*, not deleted: they never
affect the exit code, and `formwork lint` uses them for staleness detection.
Scope-level findings cannot be exempted. Markers currently apply only to
per-file checker findings; findings emitted by cross-file finalizers are
exempted by allowlist only (marker support for finalizer findings is a
phase-3b decision: implement it or have lint flag `marker: true` on
finalizer-only rules).

### Pattern semantics

- Default regex engine: Go `regexp` (RE2) — fast, linear-time.
- `syntax: regexp2` opt-in per pattern enables lookaround/backreferences
  (pure-Go engine) for the minority of rules that need PCRE2 features today. A
  regexp2 match that trips its 1s backtracking timeout **fails closed** — the
  rule errors (exit 2), never a silent no-match — because the exit-code contract
  forbids a rule that failed to evaluate from reading as a pass (#22).
- `multiline: true` matches over whole (preprocessed) file content instead of
  line-by-line.
- `prefilter:` (optional) is a **pure optimization** — a cheap literal
  substring gate that skips a file before the matcher runs. It must never
  change what the rule reports; `formwork lint` (`prefilter-load-bearing`)
  rejects a prefilter that does. Scope that changes the verdict belongs in
  `require_present:`, which is explicitly semantic. The gate applies in
  **every** `forbidden-pattern` mode — plain single-pattern rules included
  (#21; before that fix the plain path silently ignored it).

  What a prefilter owes its author (#133): the literal must be one that **every**
  string the pattern can match necessarily contains. On an alternation that means
  every branch — a literal shared by only some branches silently kills the rest,
  and on a tombstone rule nothing on the tree will ever reveal it. `formwork
  lint` proves this from fixtures and from the pattern itself, and reports a
  prefilter it cannot prove either way as **unproven** rather than passing it. A
  rule carrying a prefilter therefore owes either fire fixtures covering each
  alternative, or a pattern whose every branch carries the literal.

### Initial rule-type vocabulary

Declarative types (cover ~75–80% of the target corpus's rules):

| Type | Purpose | Key params |
|---|---|---|
| `forbidden-pattern` | pattern must not appear in scope | pattern(s) |
| `required-pattern` | anchors must appear (per-file trigger or per-scope existence) | anchors, trigger, mode |
| `pattern-count` | exactly-one / at-most-N / at-least-N across scope | pattern, op, n |
| `ordering` | pattern A must precede pattern B (file or function scope) | sequence, within |
| `pair-consistency` | trigger in unit ⇒ companion in same unit; unit is a file, a directory, or one function/block (Go, Dart, proto) | trigger, requires, where, also_present |
| `set-relation` | extract set from files A, set from files B; assert ⊆ / = / ∩=∅ | two extractors, relation |
| `file-size` | per-path-glob line caps; progressive no-grow (diff-aware) | cap table, hard cap |
| `file-naming` | forbidden extensions, required naming formats, reserved paths | rules |
| `binary-content` | magic-byte detection, size caps on tracked/staged files | allowed exts, cap |
| `doc-path-exists` | repo paths cited in docs/comments must exist | token extraction |
| `git-diff` | assertions over a diff/commit range (net-removal, base-relative naming) | range mode, patterns |
| `baseline` | compare an extraction against a tracked baseline file; `exact` (snapshot) or `shrink-only` (ratchet) modes; stale/rotten baseline entries flagged | extractor, baseline file, mode |
| `command` | run external tool, interpret exit/output; toolchain gates only | cmd, when: paths-changed, expect |

`pair-consistency` units: `where: same-file` (default), `where: same-dir`
(package-level; the one whole-tree mode), and `where: same-func` — one
top-level Go function including methods, bounded by `go/parser` spans so a
multi-line signature or a brace-bearing parameter type cannot close the unit
early, and a nested func-literal body counts as inside its enclosing
function. A package-level `var f = func(...) {...}` (including an IIFE
initializer) is also a unit, named by its var — the two-token stubbability
refactor must not take a function out of the lockdown. Package-level
initializer code OUTSIDE any func literal is deliberately not a unit:
definition sites (`const theSQL = ...`) legitimately match a trigger, and
uniting them would false-fire the rule on its own vocabulary — this residue
is the mode's one disclosed blind spot; use `same-file` where it matters.
`same-func` has a unit vocabulary for three languages. Go units are exact
(`go/parser`). Dart units are one function/method body — including a
var-bound closure at declaration scope — found by a brace-depth walk (no Go
Dart parser exists, so braces in strings/comments are indistinguishable from
code, the same heuristic contract the `dart/*` analyzers carry); containers
(`class`/`mixin`/`enum`/`extension`) open scopes, not units, so a companion
in one method cannot greenwash a bare sibling. Proto units are one
`message`/`enum` block or one `rpc` body block, with `service` as a scope;
units do not nest otherwise. Both heuristics share the mode's residue
shape: arrow-bodied Dart members, Dart collection-literal initializers, and
proto declarations outside any unit (a file-level `option`) belong to no
unit and stay unjudged — pinned by tests. A file of any other extension in
scope yields no findings (the same contract as the `go/*` analyzers), a
`.go` file that does not parse is an engine error, and so is a Dart/proto
unit whose braces never balance — never a silent skip. `also_present` — an
additional regex the unit must also match before the trigger obliges
`requires` — is accepted only with `where: same-func` and rejected at config
time (exit 2) otherwise. Origin: lifted from the validating target's
downstream fork (replacing retired shell brace-depth accumulators; our #77);
Dart/proto units were added for the same consumer.

Built-in analyzer types (the structural 22%; exact params defined per phase-4
plan, each type table-tested):

| Type | Purpose |
|---|---|
| `go/guard-precedes-call` | a guarded call must precede a sink in the same function, optionally binding a guard argument to the sink target |
| `go/call-order-in-func` | ordered call sequence within named functions |
| `go/per-func-count-relation` | per-function count coupling (e.g. stage-event inserts ≤ mirror logs) |
| `go/func-line-budget` | named function bodies ≤ N code lines / must delegate |
| `go/call-confined-to-func-name` | a symbol may only be invoked inside functions matching a name pattern |
| `sql/statement-predicate` | reassemble string-literal SQL (concat + Builder); per-statement require/forbid tokens keyed by table |
| `dart/ref-after-await` | no unguarded provider access after await in notifier methods |
| `dart/method-delegates` | methods matching a shape must delegate to a canonical helper |

**Name anchors fail closed.** An analyzer that selects its subject by name —
`funcs:` on `go/call-order-in-func` and `go/per-func-count-relation`, `method:`
on `dart/method-delegates`, and each `sequence:` stage — asserts that the anchor
matches something SOMEWHERE in the rule's scope. An anchor that matches nothing
is a finding, not silence: skipping every non-matching subject and returning
zero matches makes an empty anchor set indistinguishable from full compliance,
so an unrelated rename retires the invariant with nothing detecting the loss.
The verdict is scope-wide (in `Finalize`), never per-file — a rule scoped to a
package where only one file declares the anchored func is compliant.

`go/per-func-count-relation` additionally takes an OPT-IN `require_symbol:
left|right|both`, asserting the named side of the relation still matches a call
in scope. It is opt-in because absence is sometimes the compliant state: the
forbidden-call-in-func idiom (`funcs:` plus a banned `left:` and a
never-matching `right:` sentinel at `relation: <=`) is satisfied precisely by
`left` matching nothing. Which side constrains is the author's judgment, so the
engine takes it as a declaration rather than inferring it from the relation.

This vocabulary is the v1 engine surface. A new shape requires an engine
release and a spec update; the `command` type is the interim escape and its
usage is always visible in `lint` output. The first production port (phase 6)
maps every script to these types; a handful of highly bespoke gates (e.g. the
five-invariant Riverpod keepalive check) may justify one-off analyzer types —
acceptable, provided they live behind the same registry and fixture discipline.

## 6. Execution model

```
formwork check [--lane <name>] [--staged | --range A..B] [--rules id,…]
               [--format human|json|github] [paths…]
```

1. **Load + validate** all config. Any schema error is exit 2; never a silent
   skip.
2. **Plan**: resolve lane → rule set → union of rule scopes; resolve file-set
   mode (full tree, staged, range). Under staged/range, whole-tree-invariant
   rule types (required-pattern `exists`, set-relation, pattern-count, baseline,
   and the name-anchored analyzers `go/call-order-in-func`, `dart/method-
   delegates`, and `go/per-func-count-relation` when it carries an anchor)
   are exempt from the changeset restriction and evaluate over the tracked tree,
   because range-scoping a non-monotonic rule false-fails it (issue #4; see the
   lane/scope/orchestration design §5 "Whole-tree invariants under a changed
   set").
3. **Phase 1 — shared scan**: one filesystem walk; each in-scope file read
   once; preprocessor variants computed lazily and cached per file; all
   applicable per-file rule matchers run in a worker pool (default
   `GOMAXPROCS`). Per-file rules emit findings; cross-file rules emit
   *extractions*. The walk skips directories named `.git` and `.formwork`, at
   DIFFERENT DEPTHS, and the difference is part of the contract rather than an
   implementation detail (#268). `.git` prunes AT ANY DEPTH: it is VCS
   internals wherever it appears — a submodule's, a vendored clone's, a linked
   worktree's — and none of it is governed source. `.formwork` prunes ONLY AS A
   DIRECT CHILD OF THE WALK ROOT. There it is formwork's own config and its
   deliberately-broken fire fixtures, whose job is to contain violations, so
   skipping it is what lets `formwork test` exist at all. Deeper in the tree
   `.formwork` is ordinary governed content — a ported corpus under
   `examples/`, a vendored subproject — and it is SCANNED. The fixture argument
   does not reach the nested case: the run that owns a nested corpus's fixtures
   is the one rooted AT that directory (`formwork test -C examples/<corpus>`),
   where the same directory is a root child again and skipped again. Pruning by
   basename at any depth conflates the two, and did — measured 2026-08-26,
   2,762 of the 2,780 tracked files under `examples/` lived in
   `examples/<corpus>/.formwork/`, and the built-in skip is declarable in no
   rule's `scope.exclude`, so the `examples/**` rule #126 promised would fail
   `make check` forever reached 18 of them and none at all of the corpus where
   a ported rule is actually written. `internal/scan.builtinSkipDir` is the one
   place the depth is decided and every consult below routes through it, since
   four copies of a depth test is how a walk and its own attribution come to
   disagree about whether a path was ever looked at. A caller rendering the
   skip set into prose owns saying which depth it means: `scan.BuiltinSkipDirs`
   reports the NAMES only. The walk additionally prunes anything matching
   `scan.ignore` and, when `scan.gitignore` is declared, anything git reports
   as ignored (§5). The walk's ROOT is resolved before anything is enumerated, so a
   symlinked root (`-C alias`) scans the tree it names instead of enumerating
   nothing at exit 0, and a root that resolves to a non-directory, or does not
   resolve at all, is refused (#143 row 1). Per entry the prune order is then
   the built-in skip above, then `scan.ignore`, then `scan.gitignore`, then the
   symlink refusals — TWO of them, in order: #54's source-named one, and #143
   row 2's, which refuses a symlink leading to a directory outside the tree or
   to something the walk cannot look at (a stat/EvalSymlinks error that is not
   ENOENT — "I cannot look" is not "nothing there"). A DANGLING link names no
   content and stays skipped. A `.git`, or a ROOT-CHILD `.formwork`, spelled as
   a symlink is skipped rather than refused: those are the subtrees the walk is
   defined never to enumerate, so nothing goes unscanned by not looking, and
   refusing them would fail the ordinary linked-worktree and shared-rule-set
   repositories. The carve-out is asked at the SAME depth the prune uses, so a
   NESTED `.formwork` symlink gets the ordinary refusal: it names a subtree the
   walk would otherwise have enumerated, and skipping it silently is exactly
   the bypass the refusal exists to make loud.
   An ignored subtree is never descended: its files are absent from
   every rule, every lane, and every `--staged`/`--range` restriction. This is
   the sanctioned exception to "never a silent skip": it is not silent — every
   entry, with its reason and live match count, appears in `formwork lint`'s
   escape-hatch enumeration (§9). Inside an ignored subtree neither symlink
   refusal fires; everywhere else both are unchanged.
4. **Phase 2 — joins**: pair-consistency / set-relation / count rules assert
   over collected extractions. `command` and `git-diff` rules run here too
   (they don't consume the scan). Phase 2 dispatches through **two concurrent
   pools split by `cost:`**, not one: fast finalizers (in-process joins) keep
   the phase-1 width, while `cost: heavy` rules run at most **two at a time**
   (`min(--workers, 2)` — an operator who throttles to `--workers 1` gets a
   fully serial heavy pool, never more than they asked for).
   A heavy rule's footprint is per-*process* and unbounded from the engine's
   side — it execs an external whole-tree tool — so a `GOMAXPROCS`-wide pool
   multiplies memory rather than throughput: downstream, one run forked four
   Dart analyzers at 3.2–8.6 GB each (#67). The width is 2 rather than 1
   because the bound applies to the cost *class*, not measured weight, and the
   class is dominated by cheap shims: on the pinned production corpus (84 heavy
   rules, ~80 sub-second) full serialisation measured +48s wall (+67%), while
   width 2 keeps the analyzer worst case at two processes. The pools run
   concurrently, so bounding heavy costs fast rules no parallelism. `cost:`
   was previously only a lane filter; this is the second thing it decides.
5. **Report + exit**: findings sorted by (rule id, path, line) for
   determinism.

Output preserves the existing ecosystem contract:

- Human format: `[rule-id] OK — …` / `[rule-id] FAIL — …` with a `Cure:` block
  per failing rule. A rule whose findings are all warn-severity renders
  `[rule-id] WARN — …` (with findings and `Cure:` block) and counts as passed
  in the summary line, consistent with warn never affecting the exit code.
  Suppressed findings (spec §5 exemptions) never appear as per-rule findings
  or affect pass/fail, but the summary line appends `, N suppressed` whenever
  N > 0 (`formwork: 2/2 rules passed, 0 finding(s), 1 suppressed`) so an
  exemption in force is never invisible from `check` output alone — full
  audit detail remains `formwork lint`'s job (§9), and the `github` and
  `json` formats additionally name each suppressed finding at check time
  (#91): one `::notice` annotation per suppressed finding (after the live
  annotations; notice level can never read as a failure) and an enumerated
  `suppressed` array (rule, path, line, message, `suppressed_by` channel)
  whose length IS `summary.suppressed` — the count derives from the list,
  never asserted beside it. Neither affects pass/fail or exit codes.
- `--format github` emits `::error file=…,line=…::` annotations for live
  findings and `::notice …::suppressed: …` annotations for suppressed ones.
- `cure:` renders in all three formats (#107) — the human `Cure:` block, an
  additive `cure` field on each live JSON finding (`omitempty`: absent when
  the rule declares none, so cure-less output is byte-identical), and appended
  to the github annotation message as `%0ACure: …` (workflow commands carry no
  extra structured field, so the cure travels as message data, escaped by the
  same `%`/`\r`/`\n` rules as the message). GitHub truncates annotation lines
  past ~4096 chars *silently*, so appending a cure must never push a line
  over a 4096-**byte** line budget, charged per annotation from the real
  `::level file=…,line=…::` prefix of that finding (a long path buys the cure
  less room). Bytes, not runes: workflow-command escaping and GitHub's cap
  are byte-oriented, and bytes ≥ chars keeps the budget conservative — a
  multibyte-script cure may just show fewer than 48 visible chars. The
  finding message is never modified. If the full escaped cure
  fits, it is appended plainly; else, if at least 48 bytes (`ghMinCure`) of
  escaped cure fit alongside a
  `… (cure truncated; run formwork explain <rule-id>)` marker, the cure is
  cut to fit (never mid-escape or mid-rune — the cut backs off to a clean
  boundary, and a fragment eroded below `ghMinCure` by that backoff is
  omitted like an undersized one); else the
  cure is omitted entirely — no fragment, no marker — because a sliver is
  noise and a marker with no room of its own risks the very cap it announces.
  Cure joins at render time from the
  rule — it is never copied onto findings. Suppressed entries carry no cure in
  either format: an exempted finding is not asking to be remediated.
- Exit codes: 0 pass, 1 violations, 2 engine/config error. Rule panics are
  caught and reported as exit 2 — a crashed rule can never pass.
- `severity: warn` findings are reported but do not affect the exit code;
  only `error` findings produce exit 1.

Performance target: full-tree evaluation of all fast rules on a repo the size
of the validating target in low single-digit seconds (vs ~30s today), single
process (no fork-limit issues on Windows).

## 7. Rule self-testing (`formwork test`)

The validating target's synth-test discipline, built into the engine:

- Every rule must ship at least one `fire-*/` and one `pass-*/` fixture tree
  under `.formwork/fixtures/<rule-id>/`. Rules can be configured (per rule or
  per tag) to require ≥2 distinct fire forms — the anti-fig-leaf rule.
- Fire fixtures annotate violating lines with `// want: <rule-id>` markers
  (comment syntax per file type). Findings that have no line to annotate —
  file-level (Line 0) and scope-level (no path) findings — are declared in a
  sibling manifest `<fixture-dir>.want` (e.g. `fire-1.want` beside `fire-1/`),
  kept outside the scan root so it cannot trip the rule; entries are `path`,
  `path:line`, or `-` for scope-level, with `#`-comment lines and blank lines
  ignored. The runner asserts findings match the
  declared expectations **exactly** — no missing, no extras. This is stronger
  than the current "script exited non-zero somewhere".
- `unfireable: <reason>` is accepted only on `command`-type rules (mirrors the
  existing `UNFIREABLE` marker, which rejects that excuse for pure-pattern
  gates).
- `formwork test` runs as a plain CI step — no build-tag isolation, so the
  historical "synths compiled out, job passed vacuously" failure mode cannot
  recur.
- `formwork test [--rule <id>]` narrows a run to one rule's fixtures for the
  inner loop while porting; with no `--rule` every rule runs, so CI is
  unaffected. Selection is **fail-closed**: a `--rule` naming no configured rule
  is exit 2, never an empty run reported as "0/0 passed" — a selector that
  silently matches nothing is the same misconfigured-rule-passes hazard the
  exit-code contract exists to prevent. (`--lane` narrows the analogous way on
  `check`, §6.)
- Fixture discovery is likewise **fail-closed in the other direction** (#58, #9): a
  directory under `.formwork/fixtures/` whose name matches no rule id is a FAIL
  verdict naming every such directory — a fixture tree the per-rule loop can
  never reach is a proof that never executes, which reads as green while
  proving nothing (the symmetric counterpart of the unrecognized `fire-*`/
  `pass-*` subdir error). Other rules still run; the orphan fails the run
  (exit 1) rather than aborting it (exit 2). `--rule` runs validate against
  the FULL corpus id set, so scoping never turns sibling rules' fixtures into
  orphans. A dir matching no rule in that full set is still reported. Zero
  rules configured + orphan dirs remains exit 2 and still names the dead
  trees. (0.5.0: was a repo-wide abort at exit 2 whenever any orphan existed.)
- The engine's own rule-type implementations are additionally unit-tested in
  the formwork repo; fixture tests in adopting repos test *configurations*.
- Repo-scoped `except.allowlist` entries do not apply inside fixture trees
  (their paths are fixture-root-relative, not repo-relative, so a collision
  would silently suppress the fixture's declared finding); inline
  `formwork:allow` markers do still apply, since fixtures legitimately
  exercise marker suppression.

## 8. Orchestration

Defined in `formwork.yaml`:

- **Lanes** select rules by tags and intrinsic cost class (each rule type
  declares fast/heavy; `command` rules are heavy by default) plus a file-set
  mode. Expected mapping for the validating port: `pre-commit` = fast +
  `--staged`; `pre-merge-commit` = binary backstops, full tree; `pre-push` =
  heavy + push range; `ci` = everything. Heavy rules may declare
  `when: paths-changed: […]` (e.g. docker migration replay only when
  migrations changed).
- **`--range` is one string, tokenized shell-style.** It carries revisions and,
  optionally, a `-- <pathspec>` tail, and `internal/vcs` splits it into git
  arguments on unquoted whitespace, honouring `'…'`, `"…"` and a backslash
  escape. Quoting is the answer to #99 rather than refusal, because refusing an
  ambiguous tail cannot be spelled: a multi-pathspec tail of space-free names is
  a supported shape (#97) and is indistinguishable from one spaced name until
  the operator quotes it. Whitespace-splitting made a spaced pathspec
  unrepresentable — git received two pathspecs, matched nothing, and exited 0
  with an empty set, so every per-file rule saw zero files and the gate passed
  over an unscanned changeset. An unclosed quote or a dangling escape is exit 2,
  never a guess. What this does **not** make loud is a pathspec that matches
  nothing: that is git's honest answer to a legitimate question (scoping a range
  to a subdirectory that did not change), and no universe exists to test an
  arbitrary range's pathspec against.
- **Scope classifier**: `formwork scope [--staged|--range]` classifies a
  change as `docs | governance | runtime` from configurable path classes and
  emits language flags (`go_changed`, `dart_changed`, …). Fail-closed: any
  error or unclassified file → `runtime`. Lanes may early-exit on scope; CI
  consumes the output for job gating.

  **An empty changeset is `runtime` too, with every language flagged** (#147).
  "Any error or unclassified file" did not cover *no files at all*, and that
  gap is why the behaviour was inherited rather than chosen: `Classify` returns
  `docs` for zero paths — correctly, since `docs` **is** the classification of
  an empty set — so a wrapper routing lanes off `scope` skipped every runtime
  check whenever the changeset came back empty. `runScope` cannot distinguish a
  genuinely-empty changeset from a spuriously-empty one; both are "git
  succeeded, named nothing". The costs are not symmetric, so it assumes the
  strongest class.

  The guard belongs at the seam that *fetched* the set, not in `Classify`,
  which is a pure function over strings that did not fetch what it is judging.
  Both fail-closed arms **exit 0** — `scope` classifies, it does not gate — and
  each announces itself on stderr under its own pinned prefix, because an
  assumed classification that looks identical to a computed one is how this went
  unnoticed. The two prefixes are deliberately distinct: a git failure and an
  empty answer need different next actions from the operator.

  For `scope`'s purposes a **deletion or a rename source is a change**, so its
  changeset acquisition does not filter to `ACMR` and does not let git collapse
  a rename to its destination. `check` keeps both, because it reads file
  contents and cannot scan what is no longer there; `scope` only classifies.
- **Hooks**: `formwork hooks install` writes one generated shim per
  git-hook-named lane (`formwork check --lane pre-commit --staged`) into
  `.formwork/hooks`, and points a repo-relative `core.hooksPath`, written
  `--local`, at that directory. A shim's bytes depend on nothing but the lane,
  so it is machine-independent and meant to be committed (#146 D4). Re-running
  is idempotent and repairs a shim whose execute bit was lost. Shims formwork
  wrote for lanes the config no longer declares are removed; a file in that
  directory formwork did *not* write is reported and left where it is.
  `formwork hooks verify` replaces the hooks-wired gate: per lane, at the
  directory git names, the file must exist, be a regular file, be executable by
  the user who will commit, and byte-equal what install would write.

  **Install refuses rather than takes over.** Every refusal below is decided
  before the first write or setting. The two that find another hook runner in
  charge (D2, D7) print the exact lines that call formwork's lanes from it; D1
  has no such runner, and prints the top-level remedy instead.

  - **Not from a subdirectory (D1).** `core.hooksPath` is repo-relative and git
    resolves it *from the top level*, so an install run under a subdirectory
    writes shims into one directory and points git at another — a hook git
    never finds, reported as installed. No partial repair is offered: teaching
    the shim to pass `-C <subdir>` does not work either, because the staged
    file-set refuses a subdirectory root (`vcs.StagedPaths` calls
    `vcs.EnsureTopLevel`, deliberately, per #142). A top-level shim is not a
    compromise — it gates the subdirectory's files along with everything else.
    The refusal decides on `rev-parse --show-prefix`, which an ambient `GIT_DIR`
    used to empty — git then reported the subdirectory as the top level and
    install proceeded into exactly the state described (measured; row 5 of the
    evidence table in
    the ambient-git-environment plan).
    `internal/vcs` now removes `GIT_DIR` from the environment of the git commands
    **it** runs, so under the default policy the refusal fires on the tree `-C`
    names — and checks that removing it did not itself move the answer. Two
    limits on that sentence, both open. `internal/rules/command` builds its own
    `exec.Command` and sets no `cmd.Env`, so a `command:` rule shelling out to
    git keeps all three variables whatever `internal/vcs` decides (#177). And the
    refusal does not fire under the `FORMWORK_GIT_ENV=inherit` hatch: measured,
    `GIT_DIR=$R/.git GIT_WORK_TREE=$R/sub FORMWORK_GIT_ENV=inherit hooks install
    -C $R/sub` exits 0 reporting success and writes a repo-relative
    `core.hooksPath` git resolves to a directory that does not exist, after which
    a plain commit of a violation succeeds (#179). Removal was
    justified on the premise that the layout it breaks fails loudly (`fatal: not
    a git repository`); that holds only while no *ancestor* of `-C` is a
    repository, and where one is, git's upward discovery answers from the
    ancestor at exit 0. Measured: with a plain directory inside repository A as
    `-C` and `GIT_DIR` naming repository B, `check` went from exit 1 over a
    committed violation to exit 0, because A's `.gitignore` pruned the file B
    tracks. So the scrub is now conditional on being inert: git is asked which
    repository it resolves with and without the variables, and a difference is
    exit 2 naming both and the hatch. This is D9's axis — refuse on effect, not
    presence — applied to the pointer family, and it is what keeps `submodule
    foreach` free: that flow re-resolves to the identical git directory.

    A second family is removed with **nothing to arm** (#176): `GIT_GRAFT_FILE`,
    `GIT_REPLACE_REF_BASE`, `GIT_NO_REPLACE_OBJECTS`, `GIT_OBJECT_DIRECTORY`,
    `GIT_ALTERNATE_OBJECT_DIRECTORIES` and `GIT_SHALLOW_FILE`. These leave the
    repository git resolves untouched — git directory, shared directory and work
    tree all byte-identical — and move the *history* inside it, so the effect
    comparison above is blind to them by construction and removal is the whole
    fix. Measured on git 2.50.1, a graft file rewriting `HEAD`'s parentage turned
    `diff --name-only HEAD~1..HEAD` from one file into two, which reaches
    `check --range` and every `git-diff` rule; an alternate object directory made
    a range whose base exists only in **another** repository resolve and answer
    an empty path set at exit 0. Git sets none of the six in a context formwork
    runs in — the push quarantine that originates the object-store pair is
    server-side, and formwork installs no server-side hook. `GIT_INDEX_FILE` is
    still preserved for `--staged`'s sake, and the prune seam compares two
    indexes instead of trusting the ambient one (#175).
  - **Not over wiring this repository declared (D2).** If the repository's own
    config names a hooks directory that is not formwork's, or git is running
    hooks out of its default directory right now, install refuses — and there is
    no flag, because the project made that decision. Setting `core.hooksPath`
    would override the whole of that directory, including hook names formwork
    does not model. Once `core.hooksPath` points elsewhere the default
    directory's files are inert, so a repository that already wired formwork
    stays installable. That last allowance rests on the two reads agreeing about
    the configuration, and git's environment config overrides can separate them:
    with `GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=core.hooksPath
    GIT_CONFIG_VALUE_0=.formwork/hooks` over a repository whose local config
    says `.husky`, install read its own directory as live, called it wiring it
    may repair, and overwrote the operator's at exit 0 (measured; row 1 of the
    same evidence table).
    The pre-flight now measures that before it decides anything: three questions
    are asked twice, once with the ambient environment and once with the
    `GIT_CONFIG` family removed, and a moved answer is a refusal (exit 2, nothing
    written). The three are `vcs.HooksPath`, `vcs.TopLevel` and `vcs.CommonDir` —
    not every git call these commands make. `vcs.RepoConfig`,
    `vcs.RepoConfigWithIncludes` and `vcs.Worktrees` go unmeasured;
    `internal/hooks/gitenv.go` records that no ambient variable was found to move
    them on git 2.50.1, which is one measurement of today's git rather than a
    guarantee about it. Refusing on EFFECT rather than on which variables are set
    is what survives a variable no documentation names — `GIT_CONFIG_PARAMETERS`
    is in neither `git(1)` nor `git-config(1)` — and what stops
    `GIT_CONFIG_COUNT=0`, which changes nothing git does, costing an operator
    their install.
  - **Not over a wider-scope default silently (D7).** Where git runs hooks from
    a path this repository never declared, some setting outside it owns the
    wiring; `formwork hooks install --override-global` is the operator saying
    this repository is different. It sets `core.hooksPath` repo-locally and
    changes nothing outside, and it answers this refusal alone — never D2's.
    Formwork reads and writes no config outside the repository: the two
    questions are `rev-parse --git-path hooks` (what git will do, scope-agnostic)
    and a local-plus-worktree-scoped read (what this repository's config file
    body declares). The two are not a single coherent picture — the first
    honours git's environment config overrides and the second does not, which is
    what the pre-flight's environment measurement above refuses before either is
    asked (#167) — and the second is narrower
    than "what this repository declared": it is the **scope flag** that stops at
    the config file's own body — `git config --get core.hooksPath` follows
    includes, `git config --local --get core.hooksPath` does not (#173),
    so a `core.hooksPath` this repository sets through `include.path` reads back
    unset while git runs hooks from it. D7 therefore no longer fires on that read
    alone; D11 below separates it out first.
  - **Not on a guess about an included declaration (D11).** Where the scoped read
    reports unset but `--includes` answers, this repository declares
    `core.hooksPath` through an `include.path` directive, and install refuses —
    with no flag, like D2. It is a refusal to ANSWER rather than a verdict about
    an owner: an include is compatible with both D2's state and D7's, and
    deciding between them would mean reading configuration outside the
    repository formwork was pointed at. There is no safe rule short of that,
    because an include escapes the repository through both an absolute path and
    a relative `../../up.cfg` (measured). Two answers are compared and no
    included file is opened, so the test stays inside the repository boundary.
    The worktree scope is asked with `--includes` too: an include inside
    `.git/config.worktree` is invisible to `--local --includes` while git runs
    hooks from it. What this leaves is an include that OVERRIDES a value the
    config body also declares — the scoped read answers, so D2 judges it, on the
    included value (#173).

  A git-hook lane that selects no rules is *not* a whole-install refusal: the
  healthy lanes are wired, the empty lane gets no shim, and the run reports that
  (exit 2). A diagnosis about one lane must not remove the protection another
  was providing.

  **Worktrees are covered by D4's committed shims, not by an install-time loop
  (R8).** A relative `core.hooksPath` resolves per worktree, so a new worktree
  gets the shims from its own checkout. An install loop over `worktree list`
  would instead act on prunable and bare entries and could not be atomic across
  N worktrees. `hooks verify` is where the walk belongs. It walks the worktrees
  git lists with two deliberate omissions — a bare entry has no working tree to
  check and is passed over silently, and a worktree whose resolved hooks
  directory has already been judged (the root's included) is not judged twice —
  and every other state it finds gets its own line: unreachable, prunable,
  locked, and each per-lane problem.
- Commit-message forensics (the pre-push range checks) are `git-diff`-type
  rules assigned to the pre-push lane.

## 9. Self-integrity (`formwork lint`)

Meta-checks over the configuration itself:

- **Fixture coverage**: every rule has its required fire/pass fixtures. A heavy
  rule may carry none if it DECLARES why — `fixture_exempt: <reason>` — so the
  gap is a decision instead of an accident, and the escape-hatch census below
  prints that reason verbatim. A declaration is therefore worth exactly the
  content in it, and the field is closed on **two** surfaces rather than one:
  1. **As configuration.** `config.Load` refuses a `fixture_exempt` that is
     whitespace once trimmed, at exit 2, naming the rule — the predicate the
     engine already applies to `scan.ignore` and `scan.gitignore` reasons.
     `fixture_exempt: ""` is deliberately not refused: an empty scalar and an
     absent key decode to the same value, so nothing can tell them apart, and
     both are the undeclared state fixture-coverage reports.
  2. **As governed content.** A rule file also arrives as material some other
     run merely scans — a ported corpus, a vendored subproject, a downstream
     repository pinning an older binary — and there nothing parses it as
     configuration at all, so the refusal above never happens. That surface
     gets a formwork rule of its own
     (`.formwork/rules/fixture-exempt-declares-nothing.yaml`, #336), scoped at
     `**/.formwork/rules/**/*.yaml`, holding the quoted whitespace-only scalar
     and the block-scalar header with no body under it — the `>-` idiom with
     its reason deleted, which reads as a declaration to every reader but the
     parser. `multiline: true` is load-bearing there: whether a block
     declaration says anything is a fact about the lines BELOW the key, so a
     per-line matcher could only ever hold the quoted half and would ship as a
     placebo over the form every real declaration uses.

  The two surfaces have to agree, and a gate stricter than the loader would be
  the original defect with the operands swapped — one surface accepting a
  declaration the other rejects. The agreement is a test rather than a
  convention: `TestTheBlankDeclarationRuleAgreesWithTheLoader` runs the real
  loader and the real rule over one table of declarations and fails if either
  half moves without the other. What neither half holds is stated in the rule's
  header rather than left to be discovered: a content-bearing but meaningless
  reason (`"x"`) is a reason as far as any pattern can tell, and review is what
  catches it.
- **Lane reachability**: every rule belongs to at least one lane that runs in
  CI (no dead-on-disk rules). One manifest replaces the three must-agree
  enumerations the shell fleet needed.
- **Empty-scope rot**: a rule whose scope matches zero files in the target
  repo is flagged (ports the `frontend-gates-scan-nonempty` lesson).
- **Tracked-but-ignored (`scan-ignore-tracked`)**: a git-tracked path that
  `scan.ignore` hides — via a prune record, a file-level ignore, or (for
  paths the walk never saw: deleted trees, sparse checkouts) a direct
  glob-vs-index match — is a committed file no rule can see: a bypass that
  survives commit, review, and CI (#90). Lint fails it by path, naming the
  glob (never a bare count). Runs only when `scan.ignore` is configured. The
  tracked set is git's index (`git ls-files -z --stage`: NUL-safe against
  quoted paths, submodule gitlinks excluded — a gitlink is a pointer, not a
  file of this repo), and any git failure — including "not a git
  repository" — is an engine error (exit 2), never a silent skip. Record
  comparisons fold case on `core.ignorecase` repositories, where the index
  spelling can differ from disk by case alone. A tracked file under a
  built-in skip (`.git`, `.formwork`) is never reported on either match
  path — those are not operator-declared exemptions. Disclosed residual
  boundaries: NFC/NFD normalization divergence in non-ASCII directory names,
  and a bypass that is simultaneously case-divergent and record-free, can
  still evade; both require a deliberately crafted state well past `add -f`.
- **Exemption hygiene**: allowlist entries pointing at nonexistent paths, or
  paths that no longer trip the rule, are reported as stale; `formwork:allow`
  markers missing their mandatory reason are flagged.
- **Escape-hatch visibility**: every `command` rule and every `except` entry
  is enumerated in lint output. The enumeration also lists each currently
  suppressed finding discovered by lint's own evaluation, one line per
  suppression after that rule's other enumeration lines
  (`  <id>: suppressed <path>:<line> (<SuppressedBy>)`, omitting `:<line>`
  when the finding has none) — so an exemption silently doing its job is as
  visible as one that is merely configured. When `scan.ignore` is configured
  the enumeration opens with the repo-level channels: the built-in scan skips
  and every `scan.ignore` entry with its reason and live match count
  (`0 matches` exposes a glob that protects nothing; dir prunes report as
  pruned subtrees, never as file counts, because the walk deliberately did
  not descend to count). Records attribute to the *first* matching entry in
  config order, so an entry fully shadowed by an earlier overlapping one also
  shows `0 matches` — it genuinely removes nothing, and deleting it is safe.
  `scan.gitignore`, when configured, gets its own line in that opening block
  with the same dir/file counts and its reason; when git could not answer it
  reads `could not determine — <error>; nothing pruned`, deliberately not
  borrowing the `0 matches` sentence, which asserts that git *was* asked. Lint survives an evaluation
  error (e.g. an unreadable in-scope file) by still printing this
  enumeration before returning the error: visibility must not regress
  exactly when the repo is degraded.
- **Prefilter load-bearing**: for every rule carrying a `prefilter:`, lint
  proves the prefilter changes nothing, on three kinds of evidence. Runs only
  when a rule carries a prefilter, and reports at most one verdict per rule
  (the first arm to judge it wins; a concrete matching file outranks an
  abstract branch argument).
  1. **Real tree** — evaluate the rule with and without the prefilter over the
     repo; if stripping it changes the rule's matches, the prefilter is doing
     semantic scope work (not optimization) and is flagged.
  2. **Fixtures** (#133) — the same differential over the rule's own
     `fire-*`/`pass-*` trees. A tombstone rule matches nothing on the tree by
     construction, so arm 1 compares empty against empty and passes with no
     evidence; its fire fixtures hold what it bans and are the data the tree
     cannot supply. These runs use the same walk semantics as `formwork test`
     (no repo allowlist, no `scan.ignore`/`scan.gitignore` pruning) — a
     repo-level prune here would hide the very fixture that proves the defect.
  3. **Static implication** (#133) — parse the pattern and decide whether any
     match is possible without the literal. This covers alternation branches no
     fixture exercises, and needs no data at all. It is deliberately not a
     lexical substring test over the pattern source: it works on the parsed
     form, so an escape resolves to the character it denotes, and it reports
     only when it can name the branch that can match without the literal.
     Anything it cannot model exactly — `syntax: regexp2`, a case-folded
     literal, a branch with no guaranteed literal — is undecidable and never a
     finding on its own.

  A prefilter with **no** evidence from any arm — no findings, no fixtures, and
  a pattern arm 3 declines — is reported as **unproven**, not passed. A check
  that cannot fail is a check that passes, and this check exists to prevent
  exactly that; it must not reproduce it one level up.
- **Command-trigger armable (`command-trigger-armable`, #161)**: a `command`
  rule carrying `when.paths_changed` runs its tool only when a file the rule's
  own `scope` let through matches the trigger — the engine hands a checker
  nothing else. So a trigger that cannot intersect that scope is a gate that
  never fires, on any commit, in any mode, while reading as coverage. Lint
  fails such a rule, naming it and both glob sets. Runs only when some rule
  declares a trigger. Three verdicts, not two, because deciding glob
  intersection soundly is not glob matching and a wrong answer in the FAIL
  direction condemns a healthy rule:
  1. **Armable** — some file in the repo satisfies both the rule's `Applies`
     predicate (scope ∧ ¬exclude ∧ ¬except.paths) and a trigger glob. Green,
     and it stays green on a commit that does not touch that file: the check
     judges the repository, never the changeset.
  2. **Disjoint** — the two glob sets provably share no path, decided from
     literal directory prefixes compared segment-wise. One-sided by
     construction: it proves impossibility or says nothing, so `**/*.sql` vs
     `db/**` (which does intersect) falls through rather than being condemned.
  3. **Unproven** — the globs could intersect but nothing in this tree does.
     Reported in its own words, following the prefilter precedent above: the
     gate guards nothing here, and the cure differs from a disjoint pair's.
- **Escape-hatch qualification**: a `command` rule's enumeration line names its
  `when.paths_changed` gate when it has one — the census used to present every
  external-tool rule as unconditionally running.
- **Optional GitHub coupling**: required-status-checks manifest ↔ workflow job
  names; protected-path lists include `.formwork/**`. Actual tamper
  enforcement remains at the platform layer (non-goal above).

**Reading the tree is a precondition, not a check (#30).** Lint refuses, at exit
2, to report a verdict over a repository where a file some rule's scope selects
cannot be read. It used to depend on which checks happened to run: with no
allowlist, marker or prefilter, nothing consumed engine findings, lint read no
file, and a `0o000` in-scope file passed 3/3 — while adding a `prefilter:`, a
pure optimization that must never change a verdict, made the same repo exit 2.
The refusal is taken once, at the boundary between the path-only checks (whose
answers stand on a degraded tree, and which still print) and the checks that draw
on file content (which do not run). It reads through the same cached read the
engine uses, so it gives the identical answer and costs a subsequent engine run
nothing. Files no rule governs are not read and never refused: their contents
change no verdict lint reports.

**Corpus-selectable check set (`.formwork/lint.yaml`, #89).** A corpus may
declare, in tracked YAML beside its rules, which lint checks do not apply to it
and why:

```yaml
version: 1
skip:
  - check: empty-scope
    reason: no source tree at all, so every file-reading rule's scope is empty by construction
```

This exists because the example port corpora are fixture material — a handful of
source files each, and in `examples/palletra-port-full` none at all — so checks
that ask a question about a whole repository report hundreds of "problems" that
are properties of the corpus rather than of any rule. Over a tree with no source
files that is not most of the checkable rules but every one of them: the check
discriminates between no two rules, and the count it produces measures the
corpus. Without the mechanism `lint` could not run over `examples/` at all, and
never had.

Contract, all fail-closed:

- An absent file is the empty policy: every check runs and the output is
  unchanged. Strict decoding — an unknown field, an unsupported `version`, a
  duplicate entry, a missing `check` or a blank `reason` is exit 2.
- Each skipped check prints `[<check>] SKIPPED — <reason>` in place of its
  verdict, is excluded from the summary denominator, and is listed again in the
  summary line. Nothing is ever silently absent.
- A skipped check's WORK does not run, not merely its output line.
- A declared skip the run never reached — a typo, a renamed check, or a
  conditional check this corpus never armed — is exit 2. That is what makes the
  mechanism need no hand-maintained registry of check names: the authority on
  what lint runs is the run itself, and a skip protecting nothing is dead config.
- The unreadable-file refusal above is deliberately not skippable. It is the
  precondition for having a verdict, not a verdict.

It is declared separately from `formwork.yaml` because which checks `lint` runs
is meaningful to `lint` alone; putting it in the config every command loads would
make `check` and `test` decode a key they can never honour.

`formwork lint [--rule <id>]` narrows the per-rule checks (fixture-coverage,
empty-scope, exemption-hygiene, prefilter-load-bearing, command-trigger-armable,
and that rule's escape-hatch enumeration) to a single rule for the inner loop. The whole-config
lane checks (reachability, non-emptiness) are meaningless scoped to one rule and
are skipped under `--rule`. `scan-ignore-tracked` still runs under `--rule`:
unlike the lane checks its verdict is scoping-invariant (`scan.ignore` and the
tree are untouched by rule selection), so skipping it would only hide a real
bypass during inner-loop runs. Selection is **fail-closed** exactly as `formwork
test --rule` (§7): a `--rule` matching no rule is exit 2, never a vacuous
all-clear.

## 10. Adoption, docs, and distribution

- **`formwork init`** scaffolds `.formwork/` in a new repo: `formwork.yaml`
  with default lanes and scope classes, an example rule with fire/pass
  fixtures, and an empty allowlists directory. This is the repo-#2 onboarding
  path and keeps the generic-product promise honest.
- **`formwork list`** (shipped, #106) enumerates from the loaded config and
  the built-in registries — `list rules` (id, type, severity, cost,
  preprocess, **lane assignment**, resolved via the engine's own
  `Lane.Selects`, never a re-implementation), `list lanes`, and the
  version-skew answerers `list types` / `list preprocessors`, whose single
  source is the registry itself, so what a pinned binary reports can never
  drift from what it supports. Human and JSON formats. This *replaces*
  maintained enumeration documents (the validating target's 3,600-line
  `lockdown-enumeration.md` and the meta-gate that polices it): the manifest
  is the enumeration, including "which lane runs rule X". **`formwork
  explain <rule-id>`** (shipped, #105) shows one rule in full — scope with
  exclude justifications, params, cure/origin, lanes, exemption surface,
  fixture trees — failing loudly (exit 2) on an unknown id or an
  unrenderable rule, never printing a partial one. All three introspection
  commands load config through the same engine-version gate as check/test/
  lint: a binary the config refuses never renders its content as guidance.
- **`formwork rules-for <path>...`** (shipped, #108) is the pre-hoc guidance
  primitive: the rules governing given paths, with severity/cure/origin,
  human or JSON. It judges the same `Applies()` the engine does, so display
  cannot disagree with verdict; a path outside the root — or one naming a
  directory (including the root itself), whose bare string matches no file
  glob — is exit 2, never an empty result (an empty answer would read as
  "nothing governs this file" — a guidance fail-open); a nonexistent path is
  legitimate, because scope is a glob question and guidance is asked about
  files not yet written. A path the walk never visits (a `.git`/`.formwork`
  ANCESTOR — the built-in set prunes directories only, so a regular file
  *named* `.git`, the linked-worktree gitdir-pointer shape, is scanned and
  reported governed; a `scan.ignore` glob; a git-ignored path when
  `scan.gitignore` is declared, resolved through the same
  `meta.ResolveGitIgnore` seam the walk uses and attributed to the deciding
  `.gitignore` line in the census's `<file>:<line>:<pattern>` shape (#122 —
  a tracked file under an ignored directory keeps its governed answer, git's
  own carve-out; and because that snapshot holds only paths that exist, a
  ghost query is re-asked of `git check-ignore` directly, DECIDED by git's
  verdict on the full leaf path — which resolves every ancestor dir pattern
  and every `!`-negation exactly as it will the moment the file lands — with
  per-segment dir-frame answers refining only the prune LEVEL for
  cross-channel ordering and attribution, never deciding hiding (a pattern
  matching a directory string is not a verdict about the file beneath it).
  So a file about to be written under an ignore pattern is NOT SCANNED at
  the shallowest level the future walk would prune; a negation-carved ghost
  stays governed even when an ancestor pattern matches or the ancestor is
  collapsed in today's snapshot (the answer speaks about the moment the
  file lands, when such a collapse dissolves); and a path under a
  registered git submodule is simply governed — the walk is
  submodule-oblivious and scans whatever plain files sit there, so
  check-ignore's refusal to answer submodule pathspecs is excluded before
  the batch, never surfaced as an unanswerable channel or allowed to fail a
  sibling query's answer); or a non-regular entry at
  the leaf or any ancestor, since
  the walk skips a symlinked directory without descending — except one
  leading OUT of the tree, which `check` refuses outright) gets a loud NOT
  SCANNED answer naming the hiding channel and its declared reason
  (structural in JSON), never a rule list asserting enforcement `check` will
  not perform — attributed in the walk's own order (shallowest trigger
  first; builtin, then globs, then the gitignore prune per level) via the
  shared `scan.NotScannedBy`, so lint's census and this answer cannot
  disagree. A path that cannot be classified — a stat error that is not
  ENOENT, or an ancestor that is a regular file — is exit 2, never a
  confident answer; so is a declared `scan.gitignore` channel git cannot
  answer for — where `check` may soften (pruning nothing only widens its
  scan) but a governance answer may not — with one precedence carve-out
  matching the walk's own: a verdict decidable without git (builtin skip,
  `scan.ignore` glob, or an already-resolved snapshot) still answers, and
  only a query whose answer depends on the unanswerable data refuses,
  whether the failed piece is the snapshot resolve or the ghost re-ask.
  Because "not scanned" is a statement about the walk and not about
  external tools, the answer also names any command/git-diff rules whose
  own re-scans may still reach the path. An allowlisted path is reported as
  governed **with** its suppression (`suppressed_by`, the engine's
  `finding.SuppressedBy` shape) — except.paths omits the rule entirely,
  matching `Applies()`. On a case-insensitive filesystem the queried path is
  canonicalized to its on-disk spelling so the frame matches the walk's; on
  a case-sensitive one a divergent spelling is a genuinely different future
  file and keeps its queried frame. Formwork supplies this query only —
  briefing/injection belongs to consuming harnesses.
- **Distribution**: released, versioned static binaries per OS/arch (plus
  `go install`). Adopting repos pin an exact version wherever they install it
  (CI setup step, mise/asdf, hook shims); `formwork.yaml`'s engine-version
  constraint is the backstop — a mismatched binary refuses to run (exit 2)
  rather than evaluating rules with different semantics.

## 11. Error handling

- Config errors, missing preprocessors, unknown types, bad params → exit 2
  with the offending file/rule named.
- Rule implementation panic → recovered, reported per-rule, exit 2.
- A **supplied flag whose value the run cannot honour is exit 2**, never a
  silent fall back to the default that value was meant to replace: the operator
  typed it, so the run must either do it or say it did not. `--range ""` (#154)
  falling back to a whole-tree scan and `--workers -4` (#156) falling back to
  GOMAXPROCS are the two spellings; the second is why `engine.Run`'s
  `workers <= 0` → GOMAXPROCS stays exactly as it is — a width is all that seam
  receives, so absent-vs-unusable can only be told apart at the flag. Every
  command declaring the flag validates it (`--range`: `check` and `scope`;
  `--workers`: `check` and `test`), because the first cut of the `--range` guard
  covered one caller and left the sibling answering the opposite.
- Scope classifier is fail-closed (unknown → runtime).
- A rule whose `scope` resolves to zero files **passes** at check time by
  default — it does not fail the run — but it is **named**, not silent. The
  default is what `scope.min_files` (§5) changes, per rule and only when the
  operator arms it; the rest of this bullet describes the unarmed case, which is
  every rule that does not declare a floor. `check` emits a scan
  summary in every output format: how many files it looked at, which rules
  matched none of them, and what each declared prune channel removed. Exit codes
  are unchanged, because an empty scope is legitimate: fixture roots are small,
  and a rule scoped to a path a repo has not created yet is not a defect. That
  an unconditional failure is unaffordable is measurable —
  `examples/palletra-port-full` carries 704 rules over a tree with **no source
  files at all**, so all 570 of its non-external-tool rules are vacuous by
  construction — and by construction is the whole of it rather than most of it:
  with no files there is nothing for a scope to discriminate between, so the
  check reports every rule it can see and its count is a fact about the corpus.
  Both figures are re-derived from the tree on every run of `make
  corpus-disclosure-proof`, which also refuses a second, stale copy of this
  sentence elsewhere in the repository; until that target existed they were
  hand-typed here and had rotted while the lines around them were corrected.
  Note what they measure: a rules-only corpus, not a half-ported repo. The
  judgement that a mid-port adopter should not be failed is a design call, not a
  reading off that number.
  What was wrong was never the pass — it was that `[id] OK` at exit 0 said the
  same thing for a rule that read the tree and one that never saw a file, so
  `check` could not distinguish a clean repo from one it never looked at (#151).
  `lint`'s `empty-scope` still fails the same condition, and both now compute it
  from one predicate (`meta.RulesMatchingNoFiles`), so on a whole-tree run they
  cannot disagree about a rule — which is what they did before. Under
  `--staged`/`--range` `check` does not ask the question at all: a rule that does
  not cover this commit is irrelevant to it, not vacuous.
  - The summary is computed from the **scope predicate**, not from the prune
    census. `scope.exclude` and `except.paths` never reach the scan package, and
    an include glob that matches nothing leaves no trace anywhere — so the
    commonest real cause of a vacuous rule, a mistyped include, is invisible to
    any census-based diagnosis and to any guard written to an empty `FileSet`.
  - The census reports the two DECLARED channels only (`scan.ignore`,
    `scan.gitignore`), and that follows from what a channel IS rather than
    from what the walk records: a prune channel is built per config entry and
    keys on something the operator wrote, so a skip declared nowhere has no
    glob and no `.gitignore` line for a channel to key on. Built-in skips,
    `scope.exclude` and `except.paths` are not in it either — the walk never
    records the first, and the last two are rule fields the scan package
    never sees.
  - A symlink the walk declined to follow is that census's PEER, not one of
    its members. The walk has recorded the skip since #235, and since #309
    `check` reports it beside the channels in all three formats — its own
    field, `unfollowed_symlinks` in JSON — rather than only in `lint`'s
    escape-hatch census (§9). Being declared nowhere is both why it cannot be
    a channel and why it is owed a line of its own: otherwise a rule scoping
    `**/*.cs` over a `Program.cs` symlink reads `0 finding(s)` at exit 0 over
    a path the run never opened, with every vacuity indicator empty. It is
    disclosure only — the walk follows no symlink in any mode, and the exit
    code is unchanged.
  - The opt-in per-rule floor this bullet once deferred — so a finished port can
    turn its own empty scopes into failures — has since landed as
    `scope.min_files` (§5, #23), following `set-relation`'s `min_count`:
    permissive default, one edit per rule. Everything above still describes the
    behaviour of a rule that does not declare one, which is the default and the
    whole corpus today.
- A rule whose **checker skipped itself** is named in the same summary, in every
  format, with the checker's own reason. `command`'s `when: paths-changed` (§8)
  runs its tool only when a file **in that rule's own scope** matched the
  trigger; when none did, the tool never ran and the rule rendered `[id] OK` at
  exit 0 — the same line as a gate that ran and passed (#159). The skip stays
  correct and the exit code is unchanged; only the silence was the defect. The
  reason says *scope*, not *scan*, because the checker only ever sees files its
  scope admitted: a rule whose `scope` cannot reach its own trigger paths would
  otherwise be told the file was never scanned, one block under a summary line
  counting it.
  - Reported under `--staged`/`--range` as well, deliberately unlike the vacuity
    above: a file-set run is where a `paths-changed` trigger goes unmatched, so
    suppressing it there would suppress it where it fires. That covers file-set
    runs which carry heavy rules — a CI lane, or a bare `check --staged`. A lane
    restricted to `cost: fast`, the conventional pre-commit wiring, drops
    `command` rules before the engine, so no trigger-skip arises on that path at
    all.
  - `--skip-escapes` is the other way such a rule does not run, and it is named
    in the same list with its own reason. The drop is legitimate and stays exit
    0, but once the summary reports declined gates its silence means "none
    declined" — so a filter that removes some escapes must say so, as dropping
    *all* of them already does (exit 2, above). Lane selection is deliberately
    not reported this way: a lane not selecting a rule is selection working, not
    a rule being dropped out from under the run.
  - The two are one list with one line format, but the machine format carries a
    `channel` (`self-skip` / `skip-escapes`) so a consumer can tell them apart
    without matching prose — they differ in kind: a checker declining its own
    gate is the rule working as configured, an escape drop is an operator
    narrowing this run. Same reason `prune_channels` names its channel.
  - The question is asked of the **checker** (`rules.SkipReporter`, mirroring
    `Coster`/`Prefiltered`), never of the scope predicate: a gate declared in
    params is invisible to scope, which is why `meta.RulesMatchingNoFiles` both
    exempts external-tool rules and cannot answer this.
- Under `--staged`/`--range`, **every path git named must be accounted for**: it
  was scanned, or it is absent for a reason the walk itself owns. Anything else
  is **exit 2**. The operator asked for a specific file set and silently got a
  different one, which has no benign reading and is the same shape `--range ""`
  is already refused for. The accounting is per path, not over the changeset's
  size: one visible staged file must not excuse a missing one. Two causes, two
  messages, kept apart because their cures are opposites:
  - A **declared prune channel** hid it — cure: narrow the glob, or drop the
    path from the changeset.
  - **It never arrived** (#158) — cure: the content is not where rules read it.
    `--staged` takes the file *list* from the index and the *content* from the
    working tree, so a file staged and then removed from the worktree used to
    commit its violation at exit 0.

  Attribution asks the **filesystem before the config**. Channel attribution
  reads the configured globs, so a path that never arrived and merely happens to
  match one would be blamed on that glob — a false cause with an inert cure,
  since deleting the glob would not make an absent file scannable. `os.Lstat`
  settles it by looking, and wins. Both causes are exit 2, so the precedence
  moves only the words.

  The invariant licensing every carve-out below: **the guard fires only where a
  file-set run would cover LESS than a whole-tree run of the same repository.** A
  path the walk declines in both modes opens no coverage gap, and refusing it
  would make the pre-commit gate stricter than the CI gate it stands in for.

  **Git decides what is a pointer, not the working tree** — asking `os.Lstat`
  was a fail-open, because a regular blob whose path on disk has been
  replaced by a directory or a symlink still commits unread. Each mode therefore
  asks git about the tree it stands in for: the **index mode** under `--staged`,
  since that is what the commit will carry, and the **range's end-tree mode**
  under `--range`, read from `diff --raw` so git resolves the range rather than
  this code guessing an endpoint. `os.Lstat` only *describes* the absence.

  Git's answer is required, never guessed: if the query fails, the run is exit 2
  rather than falling back to the worktree. Both modes reach the same verdict for
  the same entity, which is the point — a gitlink is a pointer whichever
  flag was passed, and an asymmetry there hard-failed every CI range over a
  submodule bump, since a checkout without `--recurse-submodules` leaves the
  directory absent.

  Three absences are deliberately **not** refused: a path beneath a **built-in
  skip DIRECTORY** (every config-only commit is exactly this — and *directory* is
  load-bearing, since the walk consults that set for directories and for the
  symlink refusal only, so a regular file *named* `.formwork` is scanned and
  must not be excused); an
  entry **git itself calls a pointer** rather than a file (`120000` or `160000`,
  in either mode — an *unrecorded* mode is not a pointer and is refused); and an
  **empty changeset**, which asked for nothing.

  That second carve-out reaches one entry worth naming, deliberately. `WalkWith`
  consults ignore globs **before** #54's source-symlink refusal, so inside a
  declared `scan.ignore` tree the walk succeeds and a source-extension symlink
  does reach the accounting. It was refused there before #158 and is carved out
  now; the invariant holds, because the same glob prunes it in a whole-tree run.
- A run with **no rules to run at all** is never a pass. This is not the bullet
  above: that one is about one rule matching no *files*, and it stays a pass.
  Here the engine was handed nothing to enforce, so `0/0 rules passed, 0
  finding(s)` at exit 0 was a clean bill of health over a tree nothing was
  checked against (#151 rows 10–12). How each command says so differs, and the
  difference is deliberate:
  - `check`, `test`, `rules-for` and `list`'s config-derived kinds **exit 2** —
    they cannot operate at all.
    `check` names which of three causes produced it (no rules configured, a
    `--lane` that selects none, `--skip-escapes` dropping the last one); the
    others carry no such flags, so only the first reaches them. An unknown
    `--lane` was already exit 2; a lane resolving to zero rules is the same
    hazard differently spelled. `rules-for` is included because a guidance query
    answering `(none)` over a corpus it never loaded tells its caller the file is
    unconstrained — while `(none)` for a path no rule matches stays correct.
    `list rules` and `list lanes` joined them in #157, settling a question #155
    had explicitly left the other way. For `list rules` an empty enumeration IS
    truthful about what is loaded, but `-format json` makes `list` a machine
    surface, where `[]` reads as "this repository declares no guardrails". That
    argument does NOT carry `list lanes`: the condition is that the config
    loaded no rules, not that this enumeration came out empty, so a corpus
    declaring lanes and no rules refuses too — and its list would not have been
    empty. Its reason is its own: a lane over zero rules selects nothing, so no
    lane there is runnable. What both share is the family: `rules-for` refusing
    a config while `list` answers it is what actually misleads, since a consumer
    generalises from whichever member it met first.
    The three commands share one refusal (`cli.loadGatedNonEmpty`), so the
    wording cannot drift. `list types` and `list preprocessors` are deliberately
    outside it: they enumerate the built-in registries, never load config, and
    can never be empty.
  - `lint` reports it as a **check** (`rules-present`, exit 1 — its normal
    verdict for problems found), not as a refusal. Refusing preempted the checks
    that do have something to say about a rule-less config — `lane-nonempty` and
    `lane-reachability` name each dead lane, `scan-ignore-tracked` walks against
    `scan.ignore`, and the census reports its scan-channel entries — and
    asserted they would all have been vacuous, which is false whenever lanes are
    declared. It is skipped under `--rule`, where a scoped config always has a
    rule and the check could not fail.
  - Naming the cause is part of the contract, not decoration: an absent
    `.formwork/rules` and rule files that parse while declaring nothing (`rules:
    []`) are different mistakes with different cures.
  - Installing a git hook for a lane that selects no rules is refused, and
    `hooks verify` reports it: the shim runs `check --lane`, so the wiring would
    abort every commit while `verify` called it healthy.
- I/O errors on individual files fail the run (exit 2), never skip silently.

## 12. Testing & TDD strategy

Development follows red-green TDD throughout, with every new test
mutation-checked against the fix it guards (see `AGENTS.md`):

- **Rule types**: table-driven unit tests per type; every behavior lands
  test-first.
- **Preprocessors**: the Go lexer ports are tested against the documented edge
  cases of the awk originals (strings containing `//`, raw strings, runes,
  heredocs) plus the existing gatetests table cases.
- **Reporter**: golden-file tests for all three formats.
- **CLI**: end-to-end tests running the built binary against `testdata/`
  fixture repos (including git fixtures for diff/hook behavior).
- **Parity harness** (designed as `tools/parity`; **never built** — no such
  path exists, and it has been cited as real before, so treat any reference to
  it as a design name): for each ported rule, run the original
  shell script and the formwork rule against the same fixture trees
  (adapted from `freightworks/internal/gatetests/`) and diff verdicts.
  "Full parity" is measured, not asserted.

## 13. Phased delivery

Each phase gets its own implementation plan and lands shippable:

1. **Core skeleton** — config load/strict-validate, shared scan, executor,
   `forbidden-pattern` + `required-pattern`, human reporter, `formwork check`.
   End-to-end on a toy repo.
2. **Self-testing early** — `formwork test` (fixture conventions,
   want-markers), `formwork lint` basics (fixture coverage, empty-scope).
   Every subsequent rule type ships fixture-tested.
3. **Declarative completeness** — pattern-count, ordering, pair-consistency,
   set-relation, file-size/naming/binary, doc-path-exists; preprocessor lexer
   ports; full exemption taxonomy + staleness detection; JSON/GitHub formats.
4. **Built-in analyzers** — `goast` family, `sqltext`, `dartscan` families.
5. **Orchestration + escapes** — `command` type, `git-diff` rules, scope
   classifier, lanes, hooks install/verify.
6. **First production port** — rule batches by domain with parity harness
   green across all 251; author `.formwork/` config, CI workflow, and hook
   wiring for the target repo; migration/cutover notes for retiring the shell
   system and updating its meta-gates.

## 14. Risks

- **Parity ambiguity**: some shell gates have accidental behaviors (BSD-grep
  quirks, comment-stripping gaps). The parity harness compares *verdicts on
  fixtures*, not implementations; where formwork is strictly more correct, the
  fixture set is extended and the difference documented rather than bug-for-bug
  cloned.
- **Type-vocabulary creep**: bespoke one-off analyzer types are allowed but
  must pass the "could another repo conceivably instantiate this?" review; the
  lint escape-hatch report keeps pressure visible.
- **regexp2 performance**: lookaround patterns don't get RE2's linear-time
  guarantee; usage is opt-in per pattern and the shared scan amortizes cost.
- **Dart parsing depth**: the lightweight Dart lexer may hit its ceiling on the
  gnarliest lifecycle rules; the fallback is a tree-sitter-based `dartscan`
  backend (WASM, pure-Go runtime) without changing rule YAML.
