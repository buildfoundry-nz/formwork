# Design: `prefilter-load-bearing` lint check + prefilter contract

Date: 2026-07-27
Status: revised after multi-agent review (soundness / architecture /
adversarial); awaiting final sign-off.
Relates to: issue #9; extends the main design spec
(`2026-07-09-formwork-design.md`) §5, §6, §9. Sibling defects surfaced by the
review: #21 (inert plain-pattern prefilter), #22 (regexp2 timeout = silent pass).

## Problem

`prefilter:` on `forbidden-pattern` is documented as a pure optimization —
`internal/rules/pattern/forbidden.go`:

```go
Prefilter string `yaml:"prefilter"` // cheap literal gate: skip the file unless it contains this
```

The contract that phrasing implies: removing a prefilter may make a rule
slower, but must never change what it reports. **That contract is
unenforced, and two rules in the validating port already violate it** by using
the prefilter literal as a scope filter unrelated to the pattern:

| rule | prefilter | effect of removing it |
|---|---|---|
| `no-invented-material-category-default` | `platform.material_cat` | starts firing on `CategoryFloor Category = "floor"` const declarations in 2 files |
| `annotation-metrics-page-where-uses-constant` | `AnnotationMetricFromTable` | starts firing on `wall_height_groups.go`, outside the intended remit |

Verified 2026-07-27 against that port pinned at `aea137466c` (re-derive via
the differential below): 3 files disagree across those 2 rules; the other 25
prefilter-carrying rules change nothing when their prefilter is stripped.

The hazard is a maintainer acting on the documented contract — "this prefilter
is just a speed hint, it can go" — and silently altering two rules' verdicts.
the parity dossier, kept with the validating target P2 sweeps the remaining escapes for exactly this
class of optimization, so a load-bearing prefilter is a trap laid across
scheduled work. Nothing in `formwork lint` can currently tell a load-bearing
prefilter from a redundant one.

This design adds a lint check that enforces the contract, and documents the
contract where the field is defined and in the spec.

## Non-goals

- The downstream rule edits (moving the two literals to `require_present:`).
  Those live in the consuming repo and ship as a separate PR; the vendored
  copy under `projects/` is gitignored and pinned, never edited from here.
- A purely textual "prefilter literal not present in the pattern" advisory.
  Considered and rejected: it is noisy (on that corpus it flags ~5 rules, not
  the 2 real ones, because it cannot distinguish a redundant prefilter from a
  load-bearing one) and false-positives on regex escaping (a pattern
  `platform\.material_cat` does not contain the literal `platform.material_cat`
  as a substring). The behavioral check below enforces the actual contract
  instead of a lexical shadow of it.
  **Revised 2026-08-07 (#133): still rejected as stated, and the static
  implication arm added there is not it.** That arm answers a different
  question — "can any string this pattern matches omit the literal?" — from the
  *parsed* pattern, not its source text. Both objections above are answered
  rather than accepted: `regexp/syntax.Parse` resolves `\.` to a literal `.`,
  so the escaping false-positive cannot arise; and it reports only when it can
  name the branch that matches without the literal, with every construct it
  cannot model exactly (regexp2, case folding, a branch with no guaranteed
  literal) returning undecidable, which is never a finding. A lexical test
  cannot make either guarantee, which is why it stays rejected.

## The invariant

For a rule carrying a prefilter, evaluating the rule with the prefilter and
without it must produce the identical set of findings on every in-scope file.
When they differ, the prefilter is load-bearing and the check fails.

## Mechanism: behavioral differential via a second engine run

`formwork lint` already walks the target tree (`scan.Walk`) and runs the
engine. The check reuses both. The differential is expressed as **two engine
runs over the prefilter-carrying rules** — one with the rules as written, one
with their prefilters stripped — whose findings are diffed. Delegating to
`engine.Run` (rather than a hand-rolled per-file loop) is what makes the check
correct at scale; see "Why engine.Run, not a hand loop" below.

1. **Interface.** A new capability interface in `internal/rules`, in the same
   idiom as the existing `Finalizer` / `Coster` / `WholeTreeInvariant`
   opt-ins:

   ```go
   // Prefiltered is implemented by a checker carrying a literal prefilter
   // gate (spec §5). WithoutPrefilter returns an equivalent checker with the
   // gate removed, for the lint load-bearing-prefilter differential.
   type Prefiltered interface {
       Prefilter() string
       WithoutPrefilter() Checker
   }

   // PrefilterOf mirrors CostOf / IsWholeTreeInvariant: one place that knows
   // "does this checker gate, and on what literal".
   func PrefilterOf(c Checker) (literal string, ok bool)
   ```

   Implemented only by `forbidden` (required-pattern has no prefilter).
   `WithoutPrefilter()` returns a struct copy with `prefilter == ""`, reusing
   the compiled matchers. `meta` calls `PrefilterOf` and never learns *how*
   gating works. **Restriction:** `Prefiltered` is for **stateless per-file
   checkers only**. Reusing `forbidden`'s matchers in the copy (and reusing
   the already-run `r.Checker` in a second engine pass) is safe *only* because
   `forbidden` carries no per-run state and is the sole implementer — the
   opposite choice from `config.Rule.Fresh()`, which rebuilds *because*
   stateful checkers (e.g. required-pattern exists-mode) exist. If a stateful
   checker ever implements `Prefiltered`, `WithoutPrefilter()` must switch to
   a factory rebuild and the differential must run over `Fresh()` clones.

2. **Differential.** In `internal/meta/lint.go`, after the `empty-scope`
   block (it needs `fset`, not the main engine findings):

   ```
   prefilterRules := rules where PrefilterOf(r.Checker) is set
   if len(prefilterRules) == 0 { skip the check entirely }   // conditional; see 4

   strippedRules := clone of each, checker replaced by WithoutPrefilter()
   base,     errB := engine.Run(prefilterRules,  fset, 0)
   stripped, errS := engine.Run(strippedRules,   fset, 0)
   // errB/errS are handled per "Error handling" below — never swallowed.

   for each (ruleID, path, line) present in `stripped` but not in `base`:
       record (ruleID, path) as a disagreement
   ```

   A rule with any disagreement fails the check; its disagreeing paths are
   listed. Because removing a gate can only *add* matches (the gate is a top
   `return nil` before any match is constructed), `stripped ⊇ base` always, so
   a disagreement is always "stripped matches where base did not" — a
   presence diff, which is provably verdict-equivalent to a full match-set
   diff here (a non-empty `base` equals `stripped` byte-for-byte). Both
   `engine.Run` outputs are already sorted by (rule, path, line), so the diff
   and its output are deterministic without extra sorting.

3. **Faithful to preprocessing.** `engine.Run` computes each rule's
   `f.Variant(r.Preprocess)` and checks *that*. Six of the 27 prefilter rules
   in that corpus carry `preprocess:`; running the differential through
   `engine.Run` means both passes see exactly the bytes the real engine sees,
   so a prefilter literal living only in a comment (erased by `decomment-go`)
   is handled correctly — no raw-vs-preprocessed divergence. Reusing
   `engine.Run` also avoids `meta` re-implementing the engine's variant
   selection, so the differential cannot silently drift from real evaluation.

4. **Conditional, like the lane checks.** The check runs, and counts toward
   the `N/N checks passed` denominator, only when at least one rule carries a
   prefilter — mirroring how the lane checks run only when lanes exist. This
   leaves formwork's own `make lint` output unchanged (the self-host config
   has zero prefilter rules); the check is exercised by unit tests instead.

### Why engine.Run, not a hand loop

A naive per-file loop that strips the prefilter and calls `CheckFile` directly
was the first design; review found it unsound at scale:

- **Cost.** Stripping the prefilter forces the un-gated matcher over every
  in-scope file the gate normally skips. For the 6 regexp2 + `multiline` rules
  that is a backtracking full-content match over thousands of files — exactly
  the cost the prefilter exists to avoid. `engine.Run` fans this out over its
  worker pool instead of running it serially. (The total work is still real,
  but this is a lint/CI integrity check, not the `formwork check` hot path.)
- **Errors.** `f.Variant` and `CheckFile` both return errors (an unreadable
  file, a bad preprocessor). A hand loop that drops those errors would read an
  errored rule as "prefilter OK" — a breach of the exit-code non-negotiable.
  `engine.Run` already propagates them. (The prefilter gate skips the regex
  *match*, not the file read — `CheckFile` reads `Content()` before the gate in
  every mode — so both passes read every in-scope file; the cost the gate saves
  is the backtracking match, per the Cost bullet above.)

### Comparison is pre-suppression, by design

The differential compares `engine.Run`'s findings **including suppressed
ones** (it ignores the marker/allowlist `Suppressed` flag). A prefilter that
does scope work is load-bearing even if the extra matches it exposes would
later be marker- or allowlist-suppressed — the contract is about what the rule
*evaluates*, not the final post-suppression verdict. This is deliberate; the
message wording (below) reflects it by saying "matches", not "fails on".

### Error handling (fail-closed)

Any error from either `engine.Run` is surfaced as a lint engine error: `Lint`
prints the escape-hatch enumeration first (so "nothing is silently excluded"
holds even on a degraded repo, mirroring the existing `lint.go` error paths)
and then returns the error → exit 2. An `engine.Run` error is **never** folded
into the comparison as agreement and **never** silently skips a file.

### Output

Failure lines follow the existing `emit` model:

```
[prefilter-load-bearing] FAIL — 2 problem(s)
  no-invented-material-category-default: prefilter "platform.material_cat" is load-bearing — removing it makes the rule match freightworks/services/core-api/internal/metricregistry/categories.go (a match the prefilter currently suppresses); move the scope to require_present: if intended.
  annotation-metrics-page-where-uses-constant: prefilter "AnnotationMetricFromTable" is load-bearing — removing it makes the rule match freightworks/services/core-api/routes/wall_heights/wall_height_groups.go; move the scope to require_present: if intended.
```

One line per (rule, disagreeing path), so a rule load-bearing on several files
names each. `require_present:` already exists on `forbidden-pattern` and is
explicitly semantic — the right slot for scope that was smuggled into
`prefilter:`, so the message points there.

## Boundaries

- `scan` is untouched (`f.Variant` is already exported and cached).
- Gate mechanics stay in `internal/rules/pattern`; `Prefiltered` /
  `PrefilterOf` are the only new surface `meta` depends on.
- The differential is expressed via `engine.Run`, so `meta` does not
  re-implement the engine's per-file eval (no duplicated `f.Variant` line to
  drift).
- `meta` orchestrates the diff and owns the message text (report formatting),
  consistent with the existing lint checks.

## Testing (TDD, red first)

Unit tests in `internal/meta/lint_test.go`, matching the existing temp-repo
style (write `.formwork/rules/*.yaml` + source files to a temp dir, call
`Lint`, assert on output):

1. **Fire** — a rule whose prefilter literal is absent from a file its
   pattern matches ⇒ `[prefilter-load-bearing] FAIL` naming that file.
2. **Pass** — a rule whose prefilter literal appears in every file its
   pattern matches (redundant prefilter) ⇒ `[prefilter-load-bearing] OK`.
3. **Pass→fire under preprocess** — the prefilter literal appears only inside
   a comment, with `preprocess: decomment-go`, so on the preprocessed view
   the file no longer contains the literal and the rule becomes load-bearing
   ⇒ FAIL. This is the test that proves the check evaluates the preprocessed
   variant, not raw bytes.
4. **Skipped when no prefilter rules** — a config with no prefilter rule does
   not emit the check line and does not change the denominator (guards the
   conditional and formwork's own `make lint`).
5. **Suppressed match still flags** — a load-bearing prefilter whose exposed
   match is covered by an allowlist/marker still fails the check (proves the
   pre-suppression comparison).
6. **Engine error is exit 2, not OK** — an unreadable in-scope file (or bad
   preprocessor) makes the differential return an engine error, and `Lint`
   still prints the escape-hatch enumeration before returning it (proves
   fail-closed; guards the exit-code non-negotiable).

A unit test on the pattern package covers `forbidden.WithoutPrefilter()`
returning a checker that no longer gates, and `rules.PrefilterOf` reporting the
literal.

## Documentation to update in this change

- `internal/rules/pattern/forbidden.go`: the `Prefilter` field comment (and
  the struct-field comment) — state that it is a pure optimization, that
  `formwork lint` rejects a load-bearing prefilter, and that semantic scope
  belongs in `require_present:`.
- Spec `2026-07-09-formwork-design.md` §5: add the prefilter contract to the
  `forbidden-pattern` params (currently the spec does not mention `prefilter`
  at all).
- Spec §9: add a **Prefilter load-bearing** bullet to the lint check list.
- `CLAUDE.md` status line: note `formwork lint` gained `prefilter-load-bearing`.

## Known limitations and scope of protection

These are stated so the guard is not mistaken for more than it is. None are
blockers; two are tracked as their own issues because they are pre-existing
`forbidden.go` / matcher defects surfaced by this review, not caused by it.

- **Tree-dependent.** The check catches load-bearing-ness only where a
  disagreeing file is currently present in the target tree — the same standard
  as the issue's own evidence ("perishable, re-derive"). It is exactly the
  moment you want the check to fire: in CI, against the tree where a
  previously-suppressed file now matches.
- **It blocks *merging* a load-bearing prefilter, not *deleting* one.** The
  differential only runs for rules that still carry a prefilter. The moment a
  maintainer deletes the `prefilter:` line — the P2 edit this is motivated by
  — the rule no longer qualifies and this check stops examining it; detection
  then falls back to `formwork check` producing the newly-exposed finding
  (which an allowlist/marker could still suppress). The guard's real contract
  is "a load-bearing prefilter cannot land red-free", relying on it being red
  while present.
- **Latent load-bearing-ness is invisible.** If the over-matching file does
  not exist in the tree today, the prefilter reads as redundant and passes;
  the trap arms only when such a file later lands.
  **Resolved 2026-08-07 (#133):** this was scoped here as a tail risk — an
  unlucky tree state — and downstream measurement showed it is the *common*
  case. A tombstone/lockdown rule matches nothing on the tree by construction;
  that is what a tombstone is for. So for that whole class, most of a mature
  corpus, the differential compared empty against empty and the check passed
  with no evidence at all. Two arms now supply evidence the tree cannot: a
  differential over the rule's own fixture trees, and a static implication test
  over the parsed pattern (which covers branches no fixture reaches). A rule
  with no evidence from any arm is reported as unproven rather than passed.
- **regexp2 timeout can mask a disagreement (#22).** For a `syntax: regexp2`
  rule, a per-file backtracking match that hits the 1s cap is swallowed as
  "no match" by `matcher.go`, so the stripped pass can report no match where a
  match exists — a false "redundant". This is a pre-existing matcher soundness
  bug (a timed-out forbidden-pattern already reads as a pass at real check
  time) tracked in #22; this design inherits it and does not fix it. The two
  target rules use RE2 (linear, no timeout), so they are unaffected.
  **Resolved 2026-07-29 (#22):** `matcher.go` now surfaces a regexp2 timeout as
  an evaluation error (exit 2) instead of swallowing it as no-match, so the
  load-bearing check can no longer be misled by a timed-out stripped pass — both
  the gated and stripped runs error alike rather than falsely agreeing on "no
  match".
- **Plain-pattern prefilters read as removable — correctly, but for a
  different reason (#21).** A plain single-`pattern` rule never consults its
  prefilter (`forbidden.go` only gates in `all_of`/`multiline`/guarded modes),
  so the field is inert there. The differential reports "no disagreement"
  (removing an inert field changes nothing) — a correct verdict for *this*
  check's invariant, but it means the check cannot distinguish "redundant" from
  "silently ignored". The inert-gate footgun itself is tracked in #21.
  **Resolved 2026-08-04 (#21):** the gate is now consulted on the plain path
  too, so a prefilter is live in every mode and the differential genuinely
  measures it everywhere — "no disagreement" on a plain rule now means
  redundant, full stop.
- **Agreeing fixtures are evidence, not proof (#133).** The fixture arm can
  only disagree where a fixture actually exercises the branch a wrong prefilter
  drops. A rule whose fire fixtures all happen to contain the literal reports
  clean, and `ranFixtures` then suppresses the unproven verdict — so
  non-discriminating fixtures read as a pass. This is deliberate (a rule with
  fixtures has *some* evidence, and demanding proof of fixture completeness here
  would duplicate #23), and it is the reason the static arm exists: it covers the
  branches fixtures miss, for every pattern it can parse. What remains uncovered
  is the intersection — a `regexp2` or otherwise undecidable pattern whose
  fixtures are non-discriminating. That case passes silently, and fixture quality
  is #23's remit, not this check's.
- **`.gitignore`-blind.** `scan.Walk` skips only `.git`/`.formwork` (plus,
  since `scan.ignore` landed, operator-declared globs — main design spec §5),
  so the differential can flag a prefilter as load-bearing against a
  generated/vendored file. Consistent with the engine (same `fset`) and
  bounded by include globs — noise, not a wrong verdict.
