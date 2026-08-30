# Writing rules that stay honest

The judgement layer above [the reference](reference.md). The reference tells you
what a parameter does; this tells you which rules are worth writing, and how a
corpus rots when they are not.

Every practice below either names the `formwork lint` check that enforces it, or
says plainly that it is judgement with no enforcer. That distinction matters more
than the advice: **an unenforced practice is a practice you will stop following**,
and knowing which is which tells you where to spend review attention.

---

## The spine: incident → rule → fixture

A rule earns its place by closing a specific wound. The loop:

1. **Something goes wrong.** A bug ships, a review catches a class late, an
   outage traces to a shape nobody was watching for.
2. **Write the rule**, with `origin:` naming the wound and `cure:` telling the
   next person how to get out of it.
3. **Write a fire fixture holding the incident's own bytes**, and a pass fixture
   holding the fix. Not a paraphrase — the actual shape that failed.
4. **The corpus grows**, and that mistake class becomes unmakeable.

The fixture is what makes step 2 more than an assertion. A rule with no fire
fixture is a claim that some pattern is dangerous; a rule with one is a
demonstration.

> **Enforcer:** `fixture-coverage` — a rule with no fire or no pass fixture is
> reported. Heavy rules may declare `fixture_exempt: <reason>` instead, which is
> enumerated by the escape-hatch census. There is no enforcer for whether the
> fixture holds the *real* bytes; that is review's job.

---

## Check coverage before you write

The most common waste is a rule that duplicates one already in the corpus,
written because nobody looked. Before authoring:

```sh
formwork rules-for path/to/the/file.go   # what already governs this?
formwork list rules                      # the whole corpus
formwork explain <rule-id>               # one rule in full
```

Two rules covering the same shape are worse than one: they disagree eventually,
and the weaker one teaches people the class is handled.

> **Judgement, no enforcer.** Nothing detects semantic duplication — the engine
> cannot tell two patterns are about the same invariant. `rules-for` exists to
> make the check cheap, not automatic.

---

## Fixtures first, and falsify in both directions

Watching a rule fire is half the work. The other half:

- **Fire fixture** — the rule reports. Then *break the rule* and confirm the
  fixture stops firing. A fire fixture that would pass against a rule matching
  nothing proves nothing.
- **Pass fixture** — the rule is silent. Then *plant the violation* and confirm
  it fires. A pass fixture the rule could never fire on is decoration.

The failure this catches has a name in this repo: a **tautological test**, one
whose assertion recomputes the expected value the way the code does, so it passes
by construction. It is invisible to a red-then-green run, because it goes green
for the same reason the code does.

A worked example from this repo's own history: a test asserting the operator
reference documents every registered rule type **passed while covering 4 of 25**,
because rule packages self-register on import and the test binary only pulled in
a handful. Green, and asserting almost nothing. What caught it was a second
assertion about a different property, not care.

> **Enforcer:** `fixture-coverage` requires both fixtures exist. Whether either
> is falsifiable is **judgement with no enforcer** — which is exactly why the
> mutation step is a house rule rather than a suggestion.

---

## Write the cure for someone stuck at 2am

`cure:` is read by a person whose commit was just rejected and who does not have
your context. It should say what to *do*, not restate what is wrong.

```yaml
# Useless — the finding already said this.
cure: "Do not use TODO comments."

# Useful — names the alternative and where it lives.
cure: "File an issue and reference it: `// see #1234`. The gate accepts an
  issue reference because it is trackable; a bare TODO is not."
```

Two mechanical traps:

- **Quote it.** A plain YAML scalar ends at ` #`, so
  `cure: fix the accessor (audit-1 #14)` reaches the engine as
  `fix the accessor (audit-1`. Issue references are the natural thing to put in
  a cure and exactly the text YAML eats.
- **Do not claim more than the rule checks.** A cure describing a broader
  invariant than the pattern enforces teaches the reader the rule is stronger
  than it is.

> **Enforcer:** `prose-not-truncated` reports the unquoted-scalar trap. Whether
> the cure is *useful* is judgement — and it is the single highest-leverage
> judgement in a corpus, because it is the only part most people ever read.

---

## Prefilters are a contract, not an optimisation you can be casual about

A `prefilter:` literal is a promise that it changes no verdict. Get it wrong and
the rule silently stops matching a branch.

```yaml
params:
  pattern: '\bAlphaOne\b|\bbeta-two\b'
  prefilter: Alpha          # WRONG — 'beta-two' has no 'Alpha'
```

The rule now cannot fire on the second alternative, forever, and nothing about
its output says so.

> **Enforcer:** `prefilter-load-bearing`, on three kinds of evidence — a
> real-tree differential, a differential over the rule's own fixtures, and a
> static implication check on the parsed pattern. A prefilter no arm can judge is
> reported **unproven**, not passed, because a check that cannot fail is a check
> that passes.

---

## Cost classes, and when `command` is a smell

`command` and `git-diff` are `heavy`: they shell out, they are the slow half of
any lane, and they are the escape hatch reached for the highest-stakes
invariants.

**Legitimate:** the invariant genuinely needs a tool the engine does not have —
a type checker, a real parser for a language with no built-in analyzer, a
cross-repo query.

**A smell:** the invariant is expressible declaratively and someone reached for a
script because it was faster to write. You have traded a reviewable rule for an
opaque one, and lint can tell you far less about it.

Before writing a `command` rule, check whether a typed rule exists —
`formwork list types` is the authority. A typed rule that *parses* the file will
be right in cases a clever regex is wrong in, and you find those out later.

> **Enforcers:** `command-trigger-armable` reports a `when.paths_changed` that
> cannot intersect the rule's own `scope` — a gate that can never fire, in any
> mode, on any commit. The escape-hatch census enumerates every heavy rule.
> Whether the escape was *warranted* is judgement.

---

## Exemption hygiene: every channel is a ratchet

Three channels, and all three are meant to shrink:

- **markers** (`formwork:allow <id> <reason>`) — for a specific, reasoned
  exception at a specific line.
- **allowlist files** — for a known backlog you intend to burn down. Entries
  come out; they do not go in casually.
- **`except.paths`** — a scope subtraction. Blunter than the others: the rule
  never looks, so there is no finding to audit.

The failure mode is not adding an exemption. It is adding one and never
revisiting it, until the count is large enough that nobody reads the list.

> **Enforcers:** `exemption-hygiene` reports a reasonless marker and an
> allowlist entry that no longer trips the rule. `scan-ignore-tracked` fails any
> git-tracked path a `scan.ignore` glob hides. The census reports how many
> in-scope files each `except.paths` entry actually **removed**, so a dead entry
> reads `0 file(s)` instead of printing like a live one. Whether an exemption
> should still exist is judgement — the enforcers can only tell you it is dead,
> not that it is unjustified.

---

## Say what ran, not what passed

The most damaging honest-looking claim is "the gates are green" from a run that
did not evaluate what the reader assumes.

- A `--lane pre-commit` run is not the CI lane.
- `--staged` judged a changeset, not the repository.
- A skipped check is not a passed check.
- `N/M checks passed` where M shrank because a corpus declared skips is not the
  same board as yesterday's.

When reporting green, name the mode. "`make verify` exit 0" is a claim anyone can
re-run; "gates green" is not.

> **Enforcers, several, and this is where most of them live:** `rules-present`
> reports a config that enforces nothing. `lane-nonempty` reports a lane
> selecting zero rules; `lane-reachability` reports a rule no CI lane selects.
> `empty-scope` reports a rule matching no files, and `scope.min_files` turns a
> shrunken scope into a verdict rather than a disclosure. A run in which no check
> ran at all is refused rather than printed as `0/0 checks passed`.

---

## The defect class this engine exists to prevent

Read one thing from this document, read this.

Every check has two failure directions, and they are not symmetric:

- **False positive** — the rule fires on something fine. Loud, annoying,
  self-correcting: someone fixes it within a day because it blocks them.
- **False negative** — the rule reports a pass it did not earn. Silent,
  permanent, and it actively *removes* pressure to look, because the board is
  green.

A guardrail engine that fails the second way is worse than no engine, because
people trust the green. So when a rule, a check, or a fixture cannot answer a
question, the correct outcome is a refusal or an explicit **unproven** — never a
pass.

Concretely, in this codebase's own history: a symlinked fixture tree that made
the fire proof never execute while reporting `OK — 1 fixture(s)` at exit 0; a
`command` rule whose trigger could never intersect its scope, reported healthy; a
prefilter that silently narrowed a rule; a moved git index that let a *committed*
violation be pruned at exit 0. Same shape every time — something could not be
checked, and the absence of a finding was rendered as a pass.

> **Enforcer:** the exit-code contract itself (`0` pass, `1` violations, `2`
> engine or config error), and the rule that `2` must never degrade to `0`. See
> [VERSIONING.md](../VERSIONING.md) — it is the one guarantee this project would
> break a release rather than get wrong.
