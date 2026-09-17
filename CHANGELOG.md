# Changelog

## Unreleased

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
