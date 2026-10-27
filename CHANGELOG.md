# Changelog

## Unreleased

### Changed

- **`formwork test` now fails a pass fixture that could never have failed its
  rule.** `PASS-FIXTURE-VACUOUS` (the rule reaches no file of the fixture and
  would have judged nothing in it) and `PASS-FIXTURE-BLIND` (a
  `go/call-confined-to-func-name` or `go/guard-precedes-call` fixture holding no
  occurrence of the anchored symbol/sink) fail the arm and the rule, exit 1, by
  default. A corpus with such fixtures that passed before fails now; fix the
  fixture so it exercises the rule (#49).

### Added

- `formwork test` reports each pass arm's `vacuity` (`judged`, `vacuous`,
  `blind`, `not-judged`) in `--format json`, and an OK rule with `not-judged`
  pass arms (`command`, `git-diff`, any non-fast rule) says so in the human
  output instead of staying silent. The verdicts live in the new
  `internal/vacuity`, which the vacuity census now calls for
  `PASS-FIXTURE-VACUOUS` (#49).

- `formwork test --rule` takes several rules in one call, repeated
  (`--rule a --rule b`) or comma-separated (`--rule a,b`), so one corpus load
  serves the whole selection. Any unknown id is exit 2 with nothing run (#49).
- `formwork test --format json` writes a versioned (`version: 1`) per-rule,
  per-arm result document; the human output is rendered from the same
  structured results and is unchanged (#49).

## 0.12.0

### Changed (breaking)

- **A changeset run skips the whole-tree rules its change cannot reach.**
  Under `--staged`/`--range`, every whole-tree invariant evaluated over the
  tracked tree and every forbidden-pattern `census` ran its whole-root
  detector, whatever the change touched: a one-file commit on a 4,300-rule
  corpus ran 1,413 invariants and nine whole-root detectors. A rule whose
  checker declares that its inputs are its scope (`rules.ScopeBound` for an
  invariant, `rules.SubjectScoped` for a census) now runs only when the
  any-status changeset reaches a path its scope admits or a path under
  `.formwork/`. Otherwise it is named in `rules_not_run` on the new
  `changeset` channel, and is neither rendered as `OK` nor counted as passed.
  `invariant_rules` counts only the invariants that ran. `baseline` does not
  declare it and always runs. The whole-tree `check` is unchanged.

  Breaking for a hook that relied on a changeset run to report a whole-tree
  violation the change did not touch: that run now passes, and only the
  whole-tree run reports it. A census whose scope does not name every file its
  detector reads is now judged only when a changeset reaches that scope; widen
  the scope to the detector's subject.

## 0.11.0

### Removed (breaking)

- **`except.allowlist` is gone.** A file of paths whose findings a rule
  forgives carries a violation instead of curing it, so the engine no longer
  offers one. A rule declaring `except: {allowlist: …}` is now refused at load
  (exit 2, `field allowlist not found`) rather than run with the list ignored.
  The exemption channels left are inline `formwork:allow` markers and
  `except.paths`. Gone with it: the `allowlist:<file>:<line>` suppression
  attribution, `exemption-hygiene`'s stale/absent allowlist-entry arm, the
  escape-hatch census's allowlist line, `explain`'s `allowlist` field and
  `rules-for`'s `suppressed_by`. A finalizer finding is now unexemptable, as
  the census line for a marker-enabled finalizer rule says. Migration: cure
  each listed path, or move a genuinely out-of-scope path to `except.paths`.
- **The rule-corpus censuses read no known-debt list.**
  `formwork-subject-census` and `formwork-universal-cure-census` no longer open
  `.formwork/allowlists/*.txt`. Every flagged arm is a finding, as
  `formwork-anchor-census` already had it. `census.ReadDebtList` and
  `Finding.Key` are removed, and `census.Reconcile` takes no debt map.
- **`generic`'s `concurrent-tests-use-sync-primitives` has no opt-out
  comment.** `// audit-16-#5-concurrent-allowlist: <reason>` no longer
  satisfies the rule. A `*Concurrent*` test must coordinate real concurrency.

## 0.10.2

### Added

- `internal/rules/pairconsistency.SameFuncUnits(path, content)` exports the
  engine's own same-func unit geometry (name, byte span, first line) for Go,
  Dart and proto, so an instrument that reasons about which unit holds a match
  reads the engine's units instead of re-deriving them.

### Fixed

- **`pair-consistency` `where: same-func` treats an annotated Dart class as a
  container.** An annotation with arguments on the line before a class —
  `@Riverpod(keepAlive: true)` — joined the accumulated header, and the
  container test did not allow `(…)` in a leading annotation, so the class read
  as ONE function unit: every method in it shared one obligation, and a
  companion in one method answered for a sibling's trigger. Leading annotations
  are now stripped before the container and header tests, which also stops an
  annotated function (`@Deprecated('x') void f()`) being named after its
  annotation.

## 0.10.1

### Fixed

- **`pair-consistency` `where: same-func` sees Dart functions with named
  parameters.** A multi-line signature whose parameter list opens a named
  block — `String id, {` … `}) async {` — and the one-line
  `void f({int a = 1}) {` were never units: the walker took the
  named-parameter `{` for a body opener and set the real body's open depth
  before counting the `}` that closes the named block on the same line, so
  the unit closed where it opened. Every same-func rule was blind to every
  such function's body, and stayed green with its companion deleted. Only a
  `{` at parenthesis depth 0 now opens a body, braces inside a parameter list
  keep the signature accumulating, and a unit's open depth is the depth at
  its opener.

## 0.10.0

### Changed (breaking)

- **Every `-format json` answer is one object.** `list rules`, `list lanes`,
  `list types`, `list preprocessors` and `rules-for` used to print a bare
  top-level array, a shape a consumer can only decode by framing it in code.
  Each now answers an object whose one member is the list: `rules`, `lanes`,
  `types`, `preprocessors`, and `paths` (one entry per asked path, in the
  order asked). The entries are unchanged. The JSON writer refuses any other
  top-level value, so no answer can regress to an array. Migration: read the
  member instead of the document.

## 0.9.1

### Added

- `forbidden-pattern` takes `params.census`: the command runner (`cmd`,
  `when`, `expect`, `output`) whose verdict is the finding, while the rule
  type stays `forbidden-pattern` and the rule stays fast, so
  `check --skip-escapes` runs the detector. Set exactly one of `pattern`,
  `all_of`, or `census`. A `cost` key inside `census` is refused. `{{root}}`,
  `{{repo}}`, and fixture isolation apply as they do for `command`.

## 0.9.0

### Changed (breaking)

- **An excusal is a credential or evidence, never a bare string.** A
  `forbidden-pattern` `require_absent` entry (and, in `all_of` mode, a `none_of`
  entry) used to excuse any file containing its text anywhere. That made each
  one an exemption any file could claim: a copy of the text inside a string
  earned it, any number of files could hold it, and it excused triggers it had
  nothing to do with. Each entry is now exactly one of:
  - `{credential: '^…', holders: N}`: an owner. The pattern must be anchored at
    the start of a line, and `holders` is the exact number of lines in scope
    that match it. The count runs over every in-scope file, before the
    prefilter, and a difference is a finding. A rule carrying a credential is a
    whole-tree invariant, so `--staged`/`--range` evaluate it over the whole
    tree. A library rule cannot carry one, because the count belongs to the
    consuming repo.
  - `{evidence: '…', within: N}`: a discharge. It excuses only a trigger it
    starts within N lines of (N ≤ 100), and each trigger needs its own.

  A bare string, an unanchored credential, a missing or non-positive `holders`,
  or a missing or out-of-range `within` is refused at load. Migration: name each
  entry's role. An entry that proves something about the whole file is a
  requirement, not an excusal; see the next entry.

### Added

- `required-pattern` takes `when` (every-file mode only; one pattern, or a list
  that must all match) and `multiline`. "A file
  that does X must contain Y" is now a requirement in its own right instead of
  an `all_of` plus `none_of` pair.

## 0.8.2

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
