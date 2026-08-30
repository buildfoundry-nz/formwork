# Lane and Trigger Orchestration — Design

Status: **SUPERSEDED — not implemented, do not build from this.**

Written 2026-07-21 against `main`, where `formwork.yaml` is a bare
`version: 1`. That premise was wrong. Orchestration had already been built and
shipped on `origin/feat/lane-scope-orchestration` (37 commits: phases 3b, 3c,
4, 5, 6, 533/533 gates ported) and is the validating port's blocking gate
today, vendored at `tools/formwork/`.

The shipped implementation follows spec §8 as written — lanes select rules by
tags plus cost class, with a separate `scope:` classifier — rather than the two
field `stage:`/`trigger:` model below. Read
`origin/feat/lane-scope-orchestration:internal/config/config.go` for what
actually exists.

Kept as a record of an alternative that was considered and lost. Its one idea
that the shipped version does not have is §6: flagging a rule whose trigger and
scope can never both match a file in the repository. That may still be worth
lifting across; nothing else here is.

## 1. Problem

`formwork.yaml` is a bare `version: 1`. There is nowhere to record when a rule
runs or what change it responds to, so no rule can be fully ported from the
validating target even when its pattern maps cleanly onto a built rule
type. The parity dossier (kept with the validating target, not in this repo) identifies
this as blocking all 533 gates.

That corpus records the information in two columns of `gates-manifest.tsv`. Its
`lane` column partitions the fleet: go 222, always 108, dart 98, none 70,
docs 18.

## 2. Two axes, not one

The word "lane" covers two independent things:

- **When the engine runs** — a git hook stage, or CI. Spec §8's sense.
- **What changed** — whether the change touched Go, Dart, docs, or anything at
  all. The production corpus manifest's sense.

These do not imply each other. A rule that responds to Go changes might be
cheap enough for pre-commit or slow enough to defer to pre-push. Collapsing
them into one field forces a choice that the corpus does not support.

This design keeps them as two fields: `stage` and `trigger`.

`none` disappears in the process. A source gate marked `lane: none` is one
that runs only in CI, which this model expresses as `stage: ci`. It needs no
trigger vocabulary of its own.

## 3. Configuration schema

### 3.1 `formwork.yaml`

```yaml
version: 1

defaults:          # optional
  stage: ci
  trigger: always

triggers:          # optional
  go:   ["**/*.go", "**/*.sql"]
  dart: ["**/*.dart"]
  docs: ["**/*.md"]
```

`triggers` maps a name to the globs that activate it. Names are kebab-case,
validated by the same expression as rule ids.

`always` is built in and cannot be declared. Declaring it is a config error.

`defaults` supplies values for rules that omit `stage`, `trigger`, or both.
Each key is independent: a `defaults` block naming only `stage` still requires
every rule to declare its own `trigger`. The block is optional, and when it is
absent every rule must declare both fields.

The point of putting defaults here rather than building them into the binary
is auditability. A repository that wants every rule to run in CI on every
change can say so in two lines that a reader will see, and `formwork lint`
reports how many rules rely on it. The alternative — a built-in default —
makes a forgotten field indistinguishable from a deliberate one, which during
a 533-gate port would let the whole fleet drift into the slowest bucket with
nothing in the output to reveal it.

### 3.2 Rule envelope

Two new fields, both strictly decoded:

```yaml
rules:
  - id: no-weak-types
    type: forbidden-pattern
    stage: pre-commit
    trigger: go
    scope:
      include: ["**/*.go"]
```

`stage` is one of `pre-commit`, `pre-merge-commit`, `pre-push`, `ci`. The set
is fixed in the binary: git hook stages are the same in every repository, so
there is nothing repo-specific to declare.

`trigger` is `always` or a name declared in `formwork.yaml`. An undeclared
name is a config error naming the known triggers, matching how unknown rule
types and preprocessors already report.

Neither field has a built-in fallback. A rule that omits one, in a repository
whose `defaults` block does not supply it, fails to load — exit 2.

## 4. Selection

A rule runs when its stage is selected **and** its trigger is active. When
either condition fails the rule does not run, and nothing else can cause it to
run. That second half is what makes an unsatisfiable pairing detectable as
dead configuration rather than an invisible no-op.

**Stage.** `--stage ci` selects every rule, honouring spec §8's "ci =
everything". Any other value selects exactly the rules declaring that stage.
So `stage:` reads as the earliest point at which a rule fires. When `--stage`
is absent the run is a CI sweep.

**Trigger.** `always` is active whenever the command runs, including when the
changed set is empty. A named trigger is active when at least one path in the
changed set matches at least one of its globs. When there is no changed set —
a full-tree run — every trigger is active.

## 5. Changed set

A new `internal/changeset` package produces the changed set from one of:
(AS SHIPPED: this landed as `internal/cli/changeset.go` over `internal/vcs`,
not as its own package. The design's separation held; the package boundary
did not, and no consumer depends on one.)

- `--staged` — `git diff --name-only --cached`
- `--range <a>..<b>` — `git diff --name-only <a>..<b>`
- neither — no changed set; the run covers the full tree

Paths are normalised to repository-relative slash-separated form, the same
shape `scope` globs already match against.

Failure is fail-closed. If git is unavailable, the range is invalid, or the
command exits non-zero, `formwork` exits 2. An empty changed set is a valid
result and means only that no named trigger is active; it is never
manufactured from an error, because a rule that silently stops running is the
failure mode this whole design exists to prevent.

The changeset layer shells out to git and knows nothing about rules; the
engine consumes a list of paths. This keeps the existing boundary where
`scan` knows nothing about rules and rule types know nothing about lanes.

### Whole-tree invariants under a changed set

Restricting which files a rule sees is only sound for MONOTONIC rules —
forbidden-pattern and every per-file scanner — where removing files from view
can only remove findings, never add one. A whole-repo INVARIANT (required-
pattern in `exists` mode, set-relation, pattern-count, baseline) is
non-monotonic: judging it on the changed subset false-fails it as "not found" /
"subset violated" / wrong-count whenever the file bearing its token is outside
the changeset (issue #4).

So `check` partitions under `--staged`/`--range`: per-file rules range-scope to
the changed set; invariant rules evaluate over the **tracked tree** (`git
ls-files`) — not the changed subset, and not the raw working tree. Excluding
untracked/unstaged files matters: an artifact the developer is not committing
must not false-fail a pre-commit invariant (the same "don't block on content
that isn't being committed" property the changeset restriction exists for).
Rule types declare the property intrinsically via the optional
`rules.WholeTreeInvariant` interface (mirroring `Coster.Cost()`), so a new
invariant rule is covered automatically and the checker is the single source of
truth. CI's clean whole-tree run remains the authority; the local hook is a
fast approximation that sees working-tree content for tracked files (the
developer's real state), which is acceptable for a pre-commit gate.

## 6. Lint: unsatisfiable pairings

Because `ci` runs everything, asking whether a rule is reachable at all is
trivially answered yes and catches nothing. The check worth having compares a
rule's trigger against its own scope.

A rule is flagged when no file in the repository matches both its
`scope.include` and its trigger's globs. `trigger: dart` on a `**/*.go` scope
never fires, and without this check it would pass silently forever.

The check reuses the shared scan's file list rather than attempting to decide
glob intersection symbolically. This makes it repository-relative and
therefore consistent with the existing empty-scope check, which flags a rule
matching zero files by the same means. Rules with `trigger: always` are
covered by that existing check and are not re-flagged here.

`lint` additionally reports:

- triggers declared in `formwork.yaml` that no rule uses
- the count of rules relying on each `defaults` key

Both are enumerations rather than failures, consistent with how lint already
surfaces escape hatches.

## 7. CLI

```
formwork check [--stage <stage>] [--staged | --range <a>..<b>]
```

`--staged` and `--range` are mutually exclusive; supplying both is exit 2.
Neither implies a stage, and `--stage` implies neither: a pre-commit hook
passes `--stage pre-commit --staged` explicitly, which is what
`formwork hooks install` will generate when that slice lands.

The exit-code contract is unchanged: 0 pass, 1 violations, 2 engine or config
error. Selecting zero rules is a pass, not an error.

## 8. Testing

Test-first throughout, per the project's non-negotiables.

- **Config**: table tests for trigger declaration, the reserved `always` name,
  each `defaults` combination, missing and unknown values on both new fields.
  Every error path asserts exit 2 and a message naming the rule and the valid
  values.
- **Selection**: a matrix over stage × trigger × changed set, including the
  empty changed set and the no-changed-set sweep.
- **Changeset**: tests against a temporary git repository for staged and range
  modes, plus the fail-closed paths — not a repository, bad range, git
  failure.
- **Lint**: a fixture repository containing a deliberately unsatisfiable
  pairing, golden-tested output.
- **End-to-end**: `testdata/` repository exercising `--stage` and `--staged`
  against real rules.

## 9. Self-hosting

This repository's own configuration migrates in the same change, which
exercises the schema on real rules:

- `.formwork/formwork.yaml` declares `go` and `docs` triggers
- `no-todo-comments` → `stage: ci`, `trigger: go`
- `spec-exists` → `stage: ci`, `trigger: docs`

No `defaults` block: with two rules, spelling both fields out is clearer than
declaring a default, and it keeps the self-hosted config demonstrating the
explicit form.

`make verify` must stay green.

## 10. Out of scope

Deferred deliberately, each to its own slice:

- `formwork hooks install` / `verify` — consumes this design's CLI surface but
  is a separate build (spec §8).
- The `docs | governance | runtime` scope classifier and the `go-tests`,
  `dart-tests`, `generated`, `vendor` presets — the same configuration
  surface, but independent of rule selection (spec §8).
- Coupling stage to GitHub required-status-checks (spec §9).
- The 251 → 533 documentation re-baseline (kickoff P0.1) and the CLAUDE.md
  status wording (P0.2).
