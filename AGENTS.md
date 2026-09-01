# formwork

A generic guardrails engine: one Go binary (`formwork`) that evaluates
repository lockdown rules declared in tracked YAML (`.formwork/`), in place of
fleets of shell gate scripts.

This file is the operating manual for anyone — person or agent — changing this
repository. It is not the user documentation: `docs/reference.md` is the
operator reference, `docs/quickstart.md` is the tutorial, and
`docs/rule-authoring.md` is the judgement layer above them.

## Source of truth

`docs/specs/2026-07-09-formwork-design.md` — the approved design spec. Read it
before changing architecture, config schema, rule-type vocabulary, or the CLI
surface. If code must diverge from the spec, update the spec in the same change
with the reasoning.

## What exists now

**Engine core.** `internal/scan` (one shared walk; skips `.git`/`.formwork` plus
operator-declared `scan.ignore` globs, each census-enumerated with a reason and
a live match count, plus — when `scan.gitignore` is declared — every path git
reports ignored) → `internal/engine` (two-phase executor, worker pools) →
`internal/finding` → `internal/report` (human, JSON, GitHub formats).
`internal/config` decodes strictly and gates on the `engine:` semver constraint
before parsing any rule file.

The walk **refuses** rather than skips two shapes of committed symlink: one
whose own name reads as source to a toolchain (the toolchain would follow it and
compile the target, so skipping it silently was a total rule bypass), and one
leading outside the tree or to anything the walk cannot look at — because
answering "nothing there" to "I cannot look" is a fail-open. Symlinks are never
followed; refusing is the loud move. Skipped links are named in the census, with
their reason.

**Rule types — 26 registered** across `internal/rules/`: the declarative core
(`forbidden-pattern`, `required-pattern`, `pattern-count`, `ordering`,
`file-size`, `file-naming`, `binary-content`, `doc-path-exists`,
`pair-consistency`, `set-relation`, `baseline`), the built-in analyzers
(`go/*` ×5 in `goast`, `dart/*` ×4 in `dartscan`, `sql/*` ×4 in `sqlparse` +
`sqltext`), and the heavy external-tool pair (`command`, `git-diff`).

**SQL parse-tree path.** Real PostgreSQL grammar via `wasilibs/go-pgquery` (WASM
on wazero — pure Go, no cgo). The parser *is* the statement splitter, never a
naive `;` split. `internal/sqlextract` reassembles SQL composed across Go
assignment flow and reports the compositions it could not read rather than
discarding them.

**Preprocessors** behind `preprocess:`, lazily cached per file.
`formwork list preprocessors` is the authority — it reads the registry, so it
cannot drift. Any list written in prose can and does.

**Orchestration and escapes.** Lanes; `formwork check --lane <name> [--staged |
--range A..B]`; `formwork scope [<path>...] [--staged | --range A..B]`;
`formwork hooks install|verify`. Exemption
suppression (`formwork:allow` markers, allowlist files) with `formwork lint`'s
hygiene checks and an escape-hatch census that enumerates **every** channel by
which a file can escape a rule — including the ones nobody declared.

**Self-testing.** `formwork test` (fire/pass fixtures, `want:` markers, `.want`
manifests) and `formwork lint` (rules-present, fixture coverage, empty scope,
lane reachability, command-trigger-armable, and more). Both take `--rule <id>`
and fail closed on an unknown id — exit 2, never a vacuous pass.

**Introspection.** `formwork explain <rule-id>`, `formwork list
rules|lanes|types|preprocessors`, `formwork rules-for <path>...`. Types and
preprocessors come straight from the registries, so a pinned binary's reported
vocabulary cannot drift.

## Non-negotiables

- **TDD, red-green, always.** Every behaviour lands test-first.

- **Mutation-check every new test, and name the mutation.** Watching a test go
  red before the fix proves the feature is missing, not that the test guards
  what you think. Break the *fix* — restore the old behaviour, delete the guard,
  invert the condition — and confirm that specific test fails. Then say which
  mutation you used, in the PR body.

  This is not ceremony. It changed the outcome nearly every time it was done,
  including exposing tests that passed identically against the code they were
  written to condemn. That failure has a name: a **tautological test**, one
  whose assertion recomputes the expected value the way the code does, so it
  passes by construction. It is invisible to a red-then-green run because it
  goes green for the same reason the code does.

  **Check the mutation compiled.** Deleting a branch often leaves a variable
  unused, so the build fails — and a build failure reads exactly like a kill if
  only the exit code is checked. A mutation that does not compile proves
  nothing.

  **A branch the fix ADDS is the one that ships untested.** When you split one
  condition into several arms, the arms you added have nothing pointing at them
  and each looks obviously correct on the page. Enumerate them and mutate each
  separately.

  Where a guard genuinely cannot be pinned — something upstream already decides
  the case — say so in the code and say why. "Unpinnable, and here is what
  actually decides this" is worth more than a test that appears to cover it.

- **Exit-code contract**: 0 pass, 1 violations, 2 engine/config error. A
  crashed, panicking, or misconfigured rule must never read as a pass.

- **Never read a gate's verdict through anything that has its own exit status.**
  A pipe is the commonest spelling: `make verify 2>&1 | tail -40` reports
  **`tail`'s** status, so a failing suite reads as green. It is not the only
  spelling — `make verify > log 2>&1; echo "exit=$?" && git commit` has no pipe
  and fails the same way, because `&&` chains off **`echo`**, which succeeds
  whatever the gate did. `$?` is also single-use: anything between the gate and
  the read overwrites it. Use `make gate`, or redirect to a file, print the
  number, and **read it yourself** before deciding anything.

- **Ask the altitude question before writing a guard**: *at what seam does this
  invariant belong, and which callers bypass it there?* Right-idea/wrong-altitude
  fixes are the commonest way a defect comes back wearing a different hat.

- **Triage a review's findings before fixing any of them.** A review reports
  what is *true*; it has no idea what belongs in this change. Give every finding
  one of **fix now / follow-up / won't fix** and record the call.
  - **Size.** If the response is bigger than the change being reviewed, stop and
    split.
  - **Narrow beats root-cause across a boundary.** When the "real fix" reaches
    into a subsystem this change never touched, the narrow patch plus a
    follow-up is the smaller total risk.
  - **Deferring is a real answer, but the comment must not lie.** Correct the
    overclaim in the same change that defers.

- **Comments assert only about code in the diff.** This repo treats comments as
  load-bearing documentation, so an argued comment is a checkable claim and
  every extra claim is review surface you created. A statement about code you
  did not open in this change is the most reliable way to ship a false one. If
  the argument needs a fact about another file, open the file, or do not make
  the claim.

- **Strict config decoding**: unknown YAML fields are errors (exit 2).
- **Deterministic output**: findings sorted by (rule id, path, line).
- **Escape hatches stay visible**: every exemption is enumerated by
  `formwork lint`; nothing is silently excluded.

## The defect class this repo is organised around

**Fail-open**: a check reporting a pass it did not earn. Most bugs found here
have been one of these — a skipped path that read as clean, an unreadable file
counted as compliant, a rule whose scope reached nothing, a gate whose verdict
was consumed through a pipe, a test that could not fail.

When you touch error handling, skips, exemptions, filtering, or an empty result
set, ask what this code does when it **cannot answer**, and make sure the answer
is not silence. A check that cannot answer must say so rather than pass — the
codebase spells that `unproven`, `unknown`, or exit 2, never a quiet 0.

## Architecture crib

- Typed rule registry: each rule type implements one interface and registers by
  `type:` string; YAML instantiates it with strictly-validated params.
- Two-phase executor: (1) one shared filesystem scan — each file read once,
  preprocessor variants lazily cached, per-file matchers in a worker pool;
  (2) cross-file joins plus `command`/`git-diff` rules in their own pool.
- Boundaries: `scan` knows nothing about rules; rule types know nothing about
  lanes, git, or output formats; `report` owns all formatting.

## Stdlib packs (`stdlib/`, committed)

`stdlib/generic/` is the portable hygiene pack (Go/Dart/SQL rules that are not
product-specific). Adopters opt in with `library: [generic]` in
`.formwork/formwork.yaml` (requires `engine: ">= 0.6.0"`). Local rules override
pack rules by id. `make selftest` and `make lint` loop `stdlib/*/` the same way
they loop `examples/*/`. Spec: `docs/specs/2026-09-02-stdlib-library.md`.

## Example corpora (`examples/`, committed)

`make selftest` loops over `examples/*/` and runs `formwork test -C` on each, so
they gate `make verify` exactly like this repo's own rules do.

- **`examples/quickstart/`** — the teaching corpus: five rules, ten fixtures,
  each rule commented to introduce one concept. Keep it small enough to read end
  to end; new coverage belongs in the port corpora.
- **`examples/palletra-port-*`** — five scale corpora set in a fictional
  warehouse fit-out domain, each with its own `.formwork/`. 4 of the 5
  palletra-port corpora carry a source tree to check those rules against; the
  fifth, `examples/palletra-port-full`, carries none at all, so its board reads
  `formwork lint: 4/4 checks passed (2 skipped: empty-scope, exemption-hygiene —
  see .formwork/lint.yaml)` — over no files both of those skipped checks would
  report 100% of what they can see and discriminate nothing, and
  the corpus is proved by `formwork test` against its own fixtures instead.
  Every figure here — the board, its ratio, the skip list and the count of
  corpora above — is re-derived from the tree on every run of
  `make corpus-disclosure-proof`. These are *fixture material*, not vendored
  source: their oversized or otherwise odd files are the point, and repo-wide
  invariants deliberately do not reach them.

## Commands

- `make test` — full suite with the race detector. `make test PKG=./internal/engine`
  runs one package; add `RUN=<pattern>` to narrow. Both are command-line knobs
  only — never bake them into the target, and a `PKG=` matching no package is
  exit 2, never a silent empty pass.
- `make build` — build the binary to `./formwork`.
- `make check` / `make selftest` / `make lint` — self-host check, every rule
  against its fixtures, config self-integrity. All must exit 0.
- `make verify` — the full gate CI runs. `make help` lists every target with its
  description; it reads the Makefile, so unlike a list written out here it
  cannot fall behind.
- `make gate` — run the verify suite so the verdict survives a pipe. This is the
  target the "never pipe a gate" non-negotiable points at.

Iterating on one rule: `formwork test --rule <id>` and `formwork lint --rule <id>`
run just that rule's fixtures and per-rule checks. Against a large corpus that is
the difference between one rule and the whole thing per invocation.

## Symbol navigation

`gopls` and `staticcheck` are expected on PATH. This is a typed rule registry —
one interface, many implementors — so "who implements this" and "where is this
called" are a `goToImplementation` or `findReferences` away. Reach for the
language server before grep for symbol-level questions; grep is worse at exactly
the structure this codebase is built on.
