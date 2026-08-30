# Formwork Phase 3a — Preprocessors + Exemption Taxonomy (Design)

Status: approved 2026-07-11. Refines the master spec
(`2026-07-09-formwork-design.md` §5, §6, §9, §11, §12) for the first slice of
phase 3 (§13.3). Phase 3 is delivered as three plans: **3a** (this document:
preprocessor lexers, full exemption taxonomy, lint hygiene), **3b** (the eight
remaining declarative rule types), **3c** (JSON/GitHub output formats). Where
this document pins down semantics the master spec left loose, the master spec
is amended in the same implementation change.

## 1. Preprocessors

New package `internal/preprocess`: a registry of pure transforms
`name → func([]byte) []byte`.

- **Line-preserving guarantee**: output has exactly the same line count as the
  input; removed text is replaced by spaces (newlines inside removed regions
  are kept). Findings therefore always report positions in the original file.
- **`decomment-go`** — blanks `//` line comments and `/* */` block comments.
  Comment markers inside interpreted strings (`"//x"`), raw strings, and rune
  literals are not comments. Unterminated block comment runs to EOF (blanked).
- **`strings-only-go`** — inverse view: keeps only the contents of interpreted
  and raw string literals; all other text (code, comments, the quotes
  themselves) is blanked. Used by SQL-in-strings style rules.
- **`destring-sh`** — blanks the contents of single-quoted and double-quoted
  strings in shell text (quotes themselves retained), honoring backslash
  escapes inside double quotes, and blanks heredoc bodies. Exact edge-case
  behavior is settled test-first against the awk originals in the validating
  port (read-only reference) per master spec §12: strings containing `//`, raw
  strings, runes, heredocs, plus the existing `gatetests` table cases.
- Transforms are deterministic and side-effect free; the registry is populated
  at init time like the rule-type registry.

## 2. Wiring: scan variants and the `preprocess` param

- `scan.File` gains `Variant(name string) (*scan.File, error)`: a view sharing
  the file's path whose `Content()`/`Lines()` return the transformed text.
  Variants are computed lazily and cached per (file, variant); the cache is
  safe under the engine's worker pool. `Variant("raw")` (or `""`) returns the
  file itself.
- Rule envelope gains `preprocess: raw | decomment-go | strings-only-go |
  destring-sh` (default `raw`). The name is validated against the registry at
  config load; an unknown name is a config error (exit 2, rule named) — never
  a silent raw fallback.
- The engine passes `file.Variant(rule.Preprocess)` to `CheckFile`. Rule types
  remain ignorant of preprocessing (spec §4 boundary).

## 3. Exemption taxonomy (engine-level suppression)

The rule envelope's `except:` block reaches its full §5 shape:

```yaml
except:
  paths: ["**/internal/db/**"]           # existing: scope carve-out globs
  marker: true                           # honor inline formwork:allow (opt-in)
  allowlist: allowlists/pool-sites.txt   # exact-path suppression file
```

- **Inline marker** (`marker: true`): a finding is suppressed when the raw
  line it points at contains `formwork:allow <rule-id> <reason>` with a
  non-empty reason. Same line only; a marker without a reason never exempts
  (and is flagged by lint, §4 below). Marker scanning always reads the raw
  file, not the rule's preprocess variant (markers live in comments, which
  preprocessors erase).
- **Allowlist**: repo-relative slash paths, exactly one per line; `#` comments
  and blank lines ignored; no globs (globs belong in `except.paths`). The file
  path is relative to `.formwork/`. A missing or unreadable allowlist file is
  a config error (exit 2). An entry suppresses any finding of that rule whose
  `Path` equals the entry.
- **Suppression, not deletion**: after `CheckFile`/`Finalize`, the engine marks
  matching findings `Suppressed` with a `SuppressedBy` cause (`marker` or
  `allowlist:<file>:<lineno>`) instead of dropping them. Checkers never learn
  about exemptions; there is one tested choke point.
- Scope-level findings (`Path == ""`) are not exemptable by marker or
  allowlist.
- `formwork check` treats suppressed findings as passing: they do not affect
  the exit code, are not rendered per-rule, and are not counted in the
  finding tally, but its summary line appends `, N suppressed` whenever N > 0
  so the exemption is never wholly invisible from `check` alone. Per-finding
  detail (which finding, suppressed by what) remains lint's job (fix wave
  Unit DG, finding G1).
- Markers currently apply only to per-file checker findings; findings emitted
  by cross-file finalizers are exempted by allowlist only (marker support for
  finalizer findings is a phase-3b decision: implement it or have lint flag
  `marker: true` on finalizer-only rules).

## 4. Lint: `exemption-hygiene` check + escape-hatch enumeration

`formwork lint` grows from 2 to 3 checks (summary line becomes `P/3`):

- **`exemption-hygiene`** fails on:
  - an allowlist entry whose path does not exist in the repo;
  - an allowlist entry whose path exists but where the rule produces no
    finding (suppressed or live) — the entry no longer suppresses anything;
  - a `formwork:allow` marker with a matching rule id but no reason, anywhere
    in that rule's scope.
  To power staleness, lint runs the engine over the real repo once and
  inspects suppressed findings — no second exemptions-off evaluation.
- **Escape-hatch enumeration** (informational block, not a numbered check):
  every rule's `except.paths` entries, `marker: true` opt-ins, and allowlist
  attachments (with live entry counts) are listed. `command`-type rules join
  this block in phase 5.

## 5. Error handling and testing

- New failure modes follow master spec §11: unknown preprocessor, missing
  allowlist file, unreadable file during marker scan → exit 2 with the
  offending rule/file named.
- Tests: table-driven lexer tests (edge cases per §12 + the validating
  port's `gatetests` reference cases); config-param tables (preprocess
  validation, except decoding); engine suppression tests (marker, allowlist,
  reasonless marker, scope-level immunity, race run); lint tests for
  `exemption-hygiene` and the enumeration block; fixture/self-host proof that
  a marker-suppressed violation passes `formwork test` and `make verify`
  end-to-end.
- Deferred (recorded, not built): stale-marker detection (a marker on a line
  that no longer fires), glob allowlist entries, suppressed-finding rendering
  in `check` output (candidate for 3c's formats work).
