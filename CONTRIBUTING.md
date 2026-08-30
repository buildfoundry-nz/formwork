# Contributing

Thanks for looking. This is a small project with a single maintainer and an
unusually strict internal discipline, so it is worth a few minutes to read how
work enters it before you write any.

## The stance

**Issues: always welcome.** Bug reports, fail-open findings, rough edges in the
docs, "I tried to express X and couldn't" — all valuable, all wanted. A good
issue is often worth more here than a patch.

**Pull requests: by prior discussion.** Please open an issue first and get a
reply before writing code. This is not gatekeeping for its own sake — the bar
below is high enough that an unsolicited PR is very likely to need substantial
rework, and it is unkind to let someone discover that after the fact. For a
one-line typo fix, ignore this and just send it.

**Security issues do not go here.** See [SECURITY.md](SECURITY.md).

## Licensing of contributions

No CLA, no DCO tooling. Contributions are accepted under the project's licence:
per Apache-2.0 §5, anything you deliberately submit for inclusion is licensed
under [Apache-2.0](LICENSE) unless you say otherwise in the PR. Inbound equals
outbound. If your employer needs something more formal, raise it in the issue
and we will sort it out before you write code.

## The non-negotiables

These are not style preferences. A PR that misses one gets sent back regardless
of how good the underlying idea is.

### 1. Test-first, red-green, always

Every behaviour lands test-first. Write the test, **run it and watch it fail for
the right reason**, then write the minimum that makes it pass.

The "watch it fail" step is the one people skip, and it is the one that carries
the value: a test written after the code passes immediately, which proves the
test exists — not that it can catch the bug. If you did not see it red, you do
not know what it tests.

In a PR, say what you watched fail. "RED confirmed: assertion X failed with Y"
is enough.

### 2. The exit-code contract is sacred

`0` pass · `1` violations · `2` engine/config error.

A crashed, panicking, or misconfigured rule must **never** read as a pass. If
your change adds a path that can return "no findings", the reviewer's first
question will be whether that path can be reached by an error.

### 3. Fail-open is the defect class this project is organized around

The recurring bug here has one shape: **a path that reads as a pass when it owed
a failure.** An error swallowed into a no-match. A file skipped instead of
refused. A check that passes vacuously over an empty set. A filter that drops
the rules it should have run. Several of the issues in this tracker are
independent rediscoveries of it.

So when you add a `continue`, a `return nil`, an early return on an empty
collection, or an error you decide is benign — expect that to be where review
concentrates, and get ahead of it. Say in the PR why the skip is sound, or make
it loud instead:

```go
// Impossible in practice, but a silent skip here would un-check a file.
// Fail the run rather than pass one we never looked at.
return nil, fmt.Errorf("...")
```

"Loud and wrong" is recoverable. "Quiet and wrong" ships and nobody finds out.

### 4. Rule-type changes owe fixtures

A new or changed rule type is not done until it has fixtures that **discriminate**
— a `fire` case that produces the finding and a `pass` case that does not.
`formwork test` runs them; `formwork lint` fails a rule that has none.

Falsify both halves before you claim they work: break the fire fixture's
expectation and confirm it fails, cure the pass fixture's trigger and confirm it
still passes. A fixture pair that would pass either way is worse than none,
because it reads as coverage.

Adding a whole new `type:` string costs more than the rule file suggests —
registry contract, strictly-validated params, blank-import sites that drift,
fixtures, and a spec update. Ask in the issue before starting.

### 5. Strict config decoding stays strict

Unknown YAML fields are errors (exit 2), never warnings. A typo'd key that
silently disabled a guardrail is precisely the failure this tool exists to
prevent. See [VERSIONING.md](VERSIONING.md) for what that implies about schema
changes.

### 6. Output stays deterministic

Findings sort by (rule id, path, line, message). Two runs over the same tree
produce byte-identical output. Diffing runs is a supported use, so anything that
introduces map-iteration order or timing dependence into output is a bug.

### 7. Escape hatches stay visible

`command:` rules and every exemption channel are enumerated by `formwork lint`.
Nothing is silently excluded. A change that adds a way to suppress a finding
must also add its way to enumerate it.

### 8. Spec changes land in the same commit

Architecture, config schema, rule-type vocabulary, and CLI surface are governed
by the design spec under `docs/specs/`. If code diverges from the
spec, update the spec **in the same change**, with the reasoning. A spec that
lags the code is worse than no spec, because people trust it.

## Working in the repo

```sh
make build     # build ./formwork
make test      # full suite with the race detector
make verify    # everything CI runs — the gate your PR must pass
```

Narrow the inner loop with `make test PKG=./internal/... RUN=<pattern>`. Both are
command-line knobs only; `make verify` always runs the full suite.

**Do not run bare `go test ./...`.** The Makefile filters the package list for
reasons that will not be obvious until it bites you. Use `make test`.

Iterating on one rule: `formwork test --rule <id>` and `formwork lint --rule
<id>` scope to a single rule. Both fail closed on an unknown id — exit 2, never
a vacuous pass over nothing.

## Sending the PR

- One coherent change. If you find a second thing, file an issue for it.
- Commit messages explain **why**, not what — the diff already says what.
- The PR body should state: what changed, what you watched fail before it
  passed, and what you deliberately did not do.
- `make verify` green. Say so.
- Reference the issue (`Closes #N`).

Review will be direct and will probably push back on something. That is not a
judgement of the work — it is the same bar the maintainer's own changes get, and
a fair few of those get sent back too.
