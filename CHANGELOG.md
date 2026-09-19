# Changelog

## Unreleased

### Fixed

- regexp2 matches now have a 2s Go-runtime outer bound in addition to the 1s
  `MatchTimeout`. Under CPU load regexp2's userspace fastclock can starve so
  `MatchTimeout` never fires and a match flatlines for tens of minutes; the
  outer bound still fails closed (exit 2).

## 0.7.1

### Changed

- The parent-segment check moved from a load refusal to `formwork lint`
  (`command-argv-no-parent-segment`) — see the 0.7.0 entry below, which this
  amends before anyone depends on the refusal. A corpus written before the
  tokens loads again, which is what a tool reading history needs; lint runs
  on every pull request, so the shape still cannot merge.

## 0.7.0

### Changed (breaking)

- **The engine owns the tree a `command` rule reads (#28).** A rule used to
  name its tree with a path relative to the caller's working directory
  (`--root ../../..`). That is correct under `check`, where the cwd is the
  repository; under `test` the fixture runner makes the fixture tree the cwd
  and the same argv resolved to the repository again, so a pass fixture judged
  the real tree — one missing commit trailer on a branch turned into a red on
  the rule, a red from the vacuity census reporting the pair "has stopped
  discriminating", and often a third from the mutation proof.

  Three parts:

  - **`{{root}}` and `{{repo}}` argv tokens.** `{{root}}` is the tree under
    evaluation (the repository under `check`, a fixture under `test`, a scratch
    under a downstream mutation run); `{{repo}}` is the corpus's own tree, so a
    detector that lives in the repository stays reachable while judging a
    fixture. Canonical shape:
    `go -C {{repo}}/scripts/dev/x run . --root {{root}}`. Both resolve to
    ABSOLUTE paths, whatever the caller spelled — a relative one would be
    resolved by the tool against its own working directory, which is the tree
    under evaluation, so `{{repo}}` would point back at the fixture. Quote a
    token in YAML — a plain scalar cannot begin with a brace.
  - **Isolated fixtures.** A command rule's fixture arm is copied to a temp
    directory, `git init`-ed with one commit, and that copy is evaluated. Git
    discovers a repository by walking UP, so a detector running `git rev-parse`
    inside a fixture previously found whatever repository enclosed the corpus:
    an escape no argv mentions and no argv rule could close. Declarative rules
    are not copied. Isolation applies to a rule that has ADOPTED a token:
    one still naming paths relative to its caller is asking for the
    arm-inside-the-repository layout, and its detector commonly resolves a
    repo-resident helper by walking up out of the arm, so isolating it first
    would break a working fixture for no gain. An isolated run sets
    `FORMWORK_FIXTURE=1` in the tool's environment — the one fact a detector
    cannot see for itself, since an isolated fixture is a real repository with
    no upstream branch, so a commit-range plane can skip a fixture while
    staying mandatory in CI.
  - **A `..` path segment in `cmd` is reported by `formwork lint`**
    (`command-argv-no-parent-segment`). Once a rule can name its tree exactly,
    naming it relatively has no legitimate use. The check is about path
    segments: a regex like `a..b` is not reported.

    It began as a load refusal, which is stronger — a rule that does not load
    cannot read the wrong tree even once — and moved to lint because refusing
    at load makes every pre-token corpus unreadable, including to a vacuity
    census loading the corpus at a change's merge base to tell an added rule
    from an edited one. Lint runs on every pull request, so the shape still
    cannot merge; history stays readable.

  **Migration:** rewrite each `..`-bearing argv to the token form. A corpus
  that does not is refused at load with the cure in the message, so nothing
  fails silently.

## 0.6.4

### Added

- Phase 2 dispatches each pool **longest-first** (#26). A pool sends its rules
  to an unbuffered channel, so the first `width` it sends are the first
  `width` that start — and in declaration order a corpus with one dominant
  rule spends its opening slots on cheap rules while the long pole waits.
  Measured downstream over 12+ CI runs on a 4 vCPU runner: `check` is 72-80%
  of the guardrail step, two rules span 241-429s and 238-462s of a window
  whose mean concurrency is 3.1-3.3 of 4 slots, and one of the two does not
  begin until t+59/72/75s. Rules are now ranked by a previous run's measured
  duration when one is supplied, then by declared `cost:`
  (`heavy` > `tree` > `range` > `fast` — the ranking `--cost-max` already
  filters by), then not at all: the sort is stable, so rules neither key
  separates dispatch exactly as they did before.

  `check --durations <report>` supplies the measured half. The report is one a
  `check -format json` whole-tree run wrote — v0.6.2 already put a `durations`
  object in it — so a CI job feeds the next run its predecessor's report and
  keeps no state of its own. A rule the report does not name is *unknown*, not
  fast: it is dispatched after every measured rule rather than ranked against
  them at zero. The flag is refused (exit 2), never ignored, when it cannot be
  honoured — an unreadable, unparseable or timing-less report, or the flag
  alongside `--staged`/`--range`, which collect no timings and evaluate
  through a path that takes no hint.

  **Ordering is not a verdict.** Findings are sorted before they are rendered,
  the engine error is selected by declaration index rather than by which rule
  failed first, and the ordering is a permutation — so verdicts, findings and
  error selection are byte-identical with and without a hint, under any hint.
  Proved over this repository's own corpora, including the 704-rule
  `examples/palletra-port-full`, against an inverted hint
  (`TestDispatchOrderChangesNoVerdictOnTheRepoCorpora`). Without the flag,
  output is byte-identical to 0.6.3 across all seven corpora and all three
  formats. Pool widths, the heavy gate and the cost partition are unchanged
  (#67, #81, #83), as is phase 1, whose pools dispatch file indices and so
  have no rule order to recover.

  `engine.RunTimedHinted` is `RunTimed` plus the hint; `Run` and `RunTimed`
  are unchanged wrappers, so no existing caller moves.

## 0.6.3

### Added

- Cost classes are ordered, not binary (#22): `rules.Cost` gains `range` and
  `tree` between `fast` and `heavy`, with `rules.Rank`. A `command` rule
  declares its class with `params.cost` (`range` | `tree` | `heavy`); absent
  is `heavy`, so every existing corpus loads and runs unchanged, and `fast` is
  refused because a command execs. `check --cost-max <class>` runs the rules
  at or below that rank and discloses the dropped ones on the `cost-max`
  skip channel; it is exclusive with `--skip-escapes`, which keeps dropping
  every escape regardless of declared class. `rules-for` and lint's
  fixture-exemption now key on "not fast" rather than "heavy", so a declared
  `range` rule is still an external tool to both. A lane's `cost:` accepts
  the four classes and stays an exact match.

  Strict-decoding caveat (VERSIONING.md): a corpus that declares `cost:` on a
  command rule fails to load on an older binary — raise the `engine:` floor
  when you adopt it.

## 0.6.2

### Added

- `check` reports per-rule wall-clock timing. `engine.RunTimed` is `Run` plus
  per-rule durations (phase 1 summed over files, plus the phase-2 finalizer)
  and an optional per-finalizer completion callback; `Run` is an unchanged
  wrapper, so every existing caller sees identical findings and no caller is
  forced onto the new signature. `-format json` renders the timings as a
  top-level `durations` object (rule id → whole milliseconds; `omitempty`,
  so consumers that never asked keep their exact old shape), and the new
  `check -progress` flag streams one stderr line per completed finalizer
  (rule id + cumulative ms) — liveness for whole-corpus runs that otherwise
  sit silent for minutes. Timing is observability only: no finding, sort
  order, or exit code reads it. Whole-tree runs only; `--staged`/`--range`
  keep their 0.6.1 shape.

### Fixed

- `internal/cli/cli.go` exceeded the vendored 750-line cap at 764 (the
  dogfood `make check` gate, added and violated by the same commit, which the
  branch's cancelled CI run never surfaced). `rangeValueUsable` and
  `workersValueUsable` moved to `internal/cli/flags.go` — pure code motion.

## Unreleased (carried)

### Added

- `library: [generic]` in `.formwork/formwork.yaml` opts into a rule pack
  shipped inside the binary. The `generic` pack is the full portable
  Go/Dart/SQL/shell/proto hygiene inventory (`stdlib/generic/` — weak types,
  format/analyze, migrations, no committed binaries, no `skip:` in tests,
  and the rest of the inventory-generic set), proven by
  `formwork test -C stdlib/generic`. Local rules override pack rules by id.
  Unknown pack names are exit 2. `LoadRules` lives in `internal/config/library.go`
  so `config.go` stays under the 750-line vendor cap.

## 0.5.0

### Breaking

- `formwork test`: a `.formwork/fixtures/<id>/` directory that matches no live
  rule id is a FAIL verdict counted in the failed total (exit 1), not a
  repo-wide abort (exit 2), when at least one rule is configured. Other rules
  still run. Zero rules configured plus orphan dirs remains exit 2 and still
  names the dead trees. Symlink refusals and unreadable dirs remain exit 2.

  CI that treated this shape as an engine error (exit 2) will now see findings
  (exit 1). Widen or re-pin `engine:` if you constrain to a 0.4.x series.

### Fixed

- A leftover fixture directory after a rule deletion, or a TDD RED commit that
  lands the fixture dir before the rule, no longer blackouts unrelated rules'
  proofs. The orphan is still a failure; it is not a blackout.
