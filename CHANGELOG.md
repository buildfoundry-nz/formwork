# Changelog

## Unreleased

### Added

- `library: [generic]` in `.formwork/formwork.yaml` opts into a rule pack
  shipped inside the binary. The `generic` pack is the portable Go/Dart/SQL
  hygiene set (`stdlib/generic/`), proven by `formwork test -C stdlib/generic`.
  Local rules override pack rules by id. Unknown pack names are exit 2.

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
