# Quickstart: adopting formwork in a repo

This walks from nothing to a working guardrail — a rule that fires, fixtures
that prove it fires *and* that it doesn't fire on the cured code, a pre-commit
hook, and a CI lane. It takes about twenty minutes.

A complete worked version of everything below lives in
[`examples/quickstart/`](../examples/quickstart). If you would rather read the
finished thing than build it, start there — it is five rules, each one
commented to explain a different concept, and it is deliberately small enough to
read end to end.

> The other directories under `examples/` are production-scale regression
> corpora with hundreds of rules. They exist to exercise the engine, not to
> teach it. Don't start there.

---

## 1. Install a pinned binary

Download a release and verify it. **Pin the exact version** — do not track
latest. The config schema is checked against the engine version, and a floating
binary turns a schema change into a mystery failure on someone else's machine.

```sh
# Resolve the current release once, then write that exact number down —
# in your CI config, your mise/asdf file, wherever you install it.
# The list of what exists: https://github.com/buildfoundry-nz/formwork/releases
VERSION=$(gh release list --repo buildfoundry-nz/formwork --limit 1 \
            --json tagName --jq '.[0].tagName' | tr -d v)

OS=$(uname -s | tr '[:upper:]' '[:lower:]')   # linux | darwin
ARCH=$(uname -m | sed 's/x86_64/amd64/; s/aarch64/arm64/')

gh release download "v${VERSION}" \
  --repo buildfoundry-nz/formwork \
  --pattern "formwork_${VERSION}_${OS}_${ARCH}.tar.gz" \
  --pattern checksums.txt

shasum -a 256 -c checksums.txt --ignore-missing   # sha256sum -c on Linux
tar xzf "formwork_${VERSION}_${OS}_${ARCH}.tar.gz" formwork
./formwork version
```

The checksum step is not ceremony. This binary is about to run in your CI and
on every developer's machine via a git hook.

## 2. Declare the repo

Everything lives under `.formwork/` at the repo root, all of it tracked:

```
.formwork/formwork.yaml    # engine-level settings
.formwork/rules/           # one YAML file per rule (or grouped, your choice)
.formwork/fixtures/        # proof that each rule works
```

Start with the smallest possible `.formwork/formwork.yaml`:

```yaml
version: 1
engine: ">=0.3.0 <0.4.0"    # the series you installed in §1 — not a copied constant
```

**This is the one line here you should not copy blind.** Write the series you
actually installed: `formwork version` reports it, and the
[releases page](https://github.com/buildfoundry-nz/formwork/releases) is the list of what
exists. While formwork is 0.x, a minor bump may change the config schema, so the
useful shape is "at least the release I installed, and stop before the next
minor".

`engine:` is a semver constraint. It is checked **before any rule file is
parsed**, so a colleague running a stale binary gets "your formwork is too old"
instead of a confusing error about an unknown field. Set it now; it costs one
line and saves an afternoon later.

A constraint that does not match your binary is the first thing you see, and it
names both sides:

```sh
formwork check
# formwork: 0.3.0 does not satisfy engine constraint ">=0.2.0 <0.3.0" declared in formwork.yaml
echo $?   # 2
```

That is the check doing its job rather than a problem with your setup — widen
the constraint, or install the version it asks for.

Now run it, and expect it to **refuse**:

```sh
formwork check
# formwork: no rules are configured — .formwork/rules holds no *.yaml files, so
# this run would check nothing; add a rule file, or point -C at the repository
# you meant
echo $?   # 2
```

That is the whole engine in one line. A run with nothing to enforce learned
nothing about your tree, so it must not report success — `0/0 rules passed`
at exit 0 would be indistinguishable from a clean repo, and CI would go green
on a misconfiguration. You meet the same refusal in §7, where a lane that
selects no rules is also exit 2.

The boundary is worth knowing now, because it is not "anything empty is an
error". A rule that *exists* but whose `scope` matches no files still **passes**
`check` — fixture trees are small, and a rule that refused every repo it did not
match would be unusable. But it is **named**, not silent: every run reports a
scan summary saying how much it looked at, which rules matched nothing, and what
any `scan.ignore`/`scan.gitignore` entry pruned.

Here is that summary on a run with rules — this is `examples/quickstart/`, not
the repo you are building, which has no rules yet:

```sh
formwork check
# ...
# scan: 6 file(s) scanned
# formwork: 5/5 rules passed, 0 finding(s)
```

That line is there on a clean run too, and it is meant to be. `5/5 rules passed`
over `0 file(s) scanned` is not a clean repo — it is a repo nobody looked at,
and until the summary existed those two printed identically. `formwork lint`'s
`empty-scope` check (§9) still *fails* the same condition, and both compute it
from the same predicate, so on a whole-tree run they cannot tell you different
things about the same rule. (Under `--staged` `check` does not ask at all — a
rule that does not cover the commit you are making is irrelevant to it, not
vacuous.) The line is: nothing to run is an error; something to run that found
nothing to look at is disclosed by `check` and failed by `lint`.

It is still a useful thing to run here: the refusal comes *after* the config
is parsed and the `engine:` constraint is satisfied, so reaching this message
means the two things you just wrote are good. A malformed `formwork.yaml`
names the file and the field instead.

## 3. Write the first rule

Rules are YAML. Here is a complete one — no print debugging in production Go:

```yaml
# .formwork/rules/no-print-debugging.yaml
rules:
  - id: no-print-debugging
    type: forbidden-pattern
    severity: error
    scope:
      include:
        - "cmd/**/*.go"
        - "internal/**/*.go"
      exclude:
        - "**/*_test.go"
    params:
      pattern: 'fmt\.Print(f|ln)?\('
    cure: "Use the structured logger (log/slog) instead of printing to stdout. Print statements bypass log levels and are invisible in production."
    origin: "incident-2026-03-11"
    tags: [go]
```

Three fields deserve more attention than they usually get:

**`scope`** — doublestar globs from the repo root. A rule with no scope runs
over everything, which is almost never right. Scope it to where the invariant
actually holds, and the rule stops being a source of noise elsewhere.

**`cure`** — this is what somebody reads when the hook blocks their commit at
5pm. Write what to do *instead*; they can already see what they did. A rule
without a good cure gets suppressed rather than fixed, and a suppressed rule
guards nothing. This is the highest-leverage field in the file.

**`origin`** — where the rule came from: an incident, an ADR, a code review
someone got tired of repeating. Six months from now this is how the next reader
decides whether the rule still earns its place. Rules without provenance never
get deleted, because nobody dares.

Run it:

```sh
formwork check
```

Exit `0` means clean, `1` means violations, `2` means formwork itself
couldn't run. Never collapse `1` and `2` in a wrapper script — the difference
between "your repo is clean" and "the check never ran" is the whole point.

## 4. Prove the rule works

**This is the step people skip, and it is the one that matters.** A rule you
have not falsified is a rule you are guessing about. The common failure is not a
rule that misses a violation — it is a rule that can no longer fire at all
(a typo'd pattern, a scope that matches nothing) and therefore passes forever
while looking like protection.

Fixtures are tiny trees under `.formwork/fixtures/<rule-id>/`:

```
.formwork/fixtures/no-print-debugging/
  fire-1/internal/handler/orders.go    # must produce the finding
  pass-1/internal/handler/orders.go    # must not
```

In a `fire` fixture, mark the offending line:

```go
//go:build ignore

package handler

import (
	"fmt"
	"net/http"
)

func Orders(w http.ResponseWriter, r *http.Request) {
	fmt.Println("orders handler reached") // want: no-print-debugging
	w.WriteHeader(http.StatusOK)
}
```

The `pass` fixture is the same code, cured:

```go
	slog.Info("orders handler reached")
```

For findings that are about a whole file rather than one line — a missing
required pattern, a file over a size cap — there is no line to mark. Use a
sibling manifest instead, `fire-1.want`, listing the path:

```
internal/handler/orders.go
```

Run the fixtures:

```sh
formwork test
formwork test --rule no-print-debugging     # just this one, while iterating
```

### Now break them on purpose

Both halves, one at a time:

- Delete the `want:` marker from the fire fixture → `formwork test` must
  **fail** with an undeclared finding. If it still passes, your fire fixture
  isn't testing anything.
- Introduce the violation into the pass fixture → it must **fail** with an
  unexpected finding. If it still passes, your rule can't actually fire.

Put them back. That two-minute exercise is the difference between a fixture pair
that proves something and one that just sits there reading as coverage.

## 5. Add a preprocessor when the regex can't tell code from prose

Ban `panic(` and you will also fire on this:

```go
// We used to panic( here; now we return an error.
```

A false positive on a comment documenting the fix. Rather than making the regex
cleverer, change what the regex *sees*:

```yaml
    preprocess: decomment-go
```

decomment-go blanks Go comments while preserving line numbers, so findings still
point at the right line. Variants are computed once per file and shared across
every rule that asks for the same one, so this is free after the first rule
uses it. `formwork list preprocessors` shows what your binary supports.

Then make the pass fixture *contain the forbidden text in a comment*. Now the
fixture is proving the preprocessor is load-bearing, not decorative — remove the
`preprocess:` line and that fixture fails.

## 6. Not everything is a regex

`formwork list types` enumerates what the binary you installed can do. Reach for
a typed rule before a clever regex:

| want to express | type |
|---|---|
| this text must not appear | `forbidden-pattern` |
| this text must appear | `required-pattern` |
| files must stay under N lines | `file-size` |
| if X appears, Y must too | `pair-consistency` |
| these two sets must correspond | `set-relation` |
| this must come before that | `ordering` |
| Go/Dart/SQL structural facts | the `go/*`, `dart/*`, `sql/*` analyzers |
| none of the above | `command` (runs an external tool) |

A regex that approximates "this Go file declares two functions with the same
name" will be wrong in ways you discover much later. A rule type that parses the
file will not.

`command` rules are the escape hatch, and they are deliberately conspicuous:
`formwork lint` enumerates every one, because a rule that shells out is a rule
whose behaviour isn't visible in the config. Use it when you must; expect to
justify it. Note also that anyone who can merge to `.formwork/` can therefore
execute code in your CI — see [SECURITY.md](../SECURITY.md).

## 7. Lanes: the same rules at different moments

A lane selects rules. It never changes what a rule means, so a finding is the
same finding whichever lane surfaced it.

```yaml
# .formwork/formwork.yaml
lanes:
  pre-commit:
    all: true
    cost: fast      # skip anything that shells out — keep the hook quick
  ci:
    all: true
    ci: true        # everything, including heavy rules
  migrations:
    tags: [sql]     # only rules tagged sql
```

```sh
formwork check --lane pre-commit
```

Keep the hook lane fast. A pre-commit hook that takes thirty seconds gets
bypassed with `--no-verify`, and a bypassed hook is worth less than no hook —
it produces the *feeling* of protection with none of it.

> **Add a tag lane only once you have rules carrying that tag.** A lane that
> selects nothing is not a harmless no-op — it is refused. If you copy the
> `migrations` lane above into a repo with no `sql`-tagged rules,
> `formwork check --lane migrations` exits 2 naming the lane, and `formwork
> lint`'s `lane-nonempty` check reports it too. That matters most where §8 is
> about to put it: an empty lane wired into a git hook or a CI job would
> otherwise be a green step that ran nothing, on every commit, indefinitely.

## 8. Wire it up

**Git hooks.** A lane named after a git hook (`pre-commit`, `pre-push`,
`pre-merge-commit`) can be installed as one:

```sh
formwork hooks install     # writes shims to .formwork/hooks, sets core.hooksPath
formwork hooks verify      # asserts the installed shims are current — run this in CI
```

`hooks verify` matters more than it looks: it catches the developer whose hooks
silently stopped being installed three weeks ago.

Run `hooks install` from the repository's top level. `core.hooksPath` is
repo-relative and git resolves it from the top level, so an install run in a
subdirectory would write shims into a directory git never looks in — formwork
refuses that instead. The top-level shim gates the subdirectory's files too.
(Formwork asks git where the top level is, and it removes `GIT_DIR`,
`GIT_WORK_TREE` and `GIT_COMMON_DIR` from the environment of the git commands it
runs to answer questions about your repository. An ambient `GIT_DIR` or
`GIT_WORK_TREE` beats the directory you named and would answer for a different
repository. `GIT_COMMON_DIR` is different in kind and is removed for a different
reason: measured, it leaves the git directory, the top level and `ls-files` at
the repository you named, and redirects what git *reads* — the local config and
`info/exclude`, which are exactly what formwork reads to decide whose hook wiring
is in force. (A `command:` rule that shells out to git is not one of those: it
runs your argv with the environment as you set it, #177.) Removing them is only
safe while it changes
nothing, so formwork checks: if one of those variables is set, it asks git which
repository it resolves both with and without them, and **refuses** when the two
differ rather than picking a side. That refusal is exit 2 wherever git's answer
decides the wiring or the file set — `hooks install`, `hooks verify`,
`check --staged`, `check --range`. A whole-tree `check` whose only use of git is
`scan.gitignore` does not exit 2: nothing is pruned, the census line reads *"could
not determine … nothing pruned"*, and the whole tree is scanned, so the run
scans more rather than less and its exit code is the rules' own verdict.
`submodule foreach` sets `GIT_DIR` and
re-resolves to the same repository, so it stays silent. If your layout needs the
variables honoured, `FORMWORK_GIT_ENV=inherit` turns the removal off, loudly.
That hatch exists for one layout — a bare repository plus a work tree, joined
only by `GIT_DIR` and `GIT_WORK_TREE` — so it is not a general off-switch. The
gate that decides is narrower than that description: it tests that
`GIT_WORK_TREE` is set and that git answers `./` for the top level at the
directory `-C` names, and it does not test that the repository is bare. Anything
that fails those two still refuses rather than answering about a repository you
did not name — including `GIT_DIR` on its own, which names no work tree at all
and leaves git treating your current directory as one, and including a git too
old to answer the question formwork asks, where it refuses rather than
proceeding on an answer it never got. What passes those two is not safe on its
own, so `hooks install` asks git one more question before it writes anything.
With `GIT_WORK_TREE` naming a *subdirectory* of a non-bare repository, git
answers `./` for the top level there and every other guard agrees — while a
plain `git commit`, which carries none of these variables, resolves the
repo-relative `core.hooksPath` from the *real* top level and finds no shim at
all. So install compares the root you named against the worktrees the repository
itself registers, and exits 2 naming both when the registry does not list it,
leaving the resolution to you: run it with `-C` naming one of those worktrees,
or unset `FORMWORK_GIT_ENV` so formwork resolves this repository the way `git
commit` does. The layout the hatch exists for registers no work tree at all — a
bare repository plus a detached work tree — so that one installs. Issues #167
and #179.)

`hooks install` never takes over hook wiring that is already there. If this
project has already declared its own — a `core.hooksPath` in this repository's
config, or a hook git is already running from its default hooks directory —
install refuses and tells you the line to add to your existing hook instead.
There is no flag for that: it is the project's decision, not formwork's.

A declaration your repository makes through an `include.path` directive gets the
same answer, for a different reason. Formwork reads the body of your local config
and does not follow includes, so it can see that git resolves `core.hooksPath`
through one but not whose setting it is — an include can name a file anywhere on
the machine. It refuses rather than guessing, and there is no flag for that
either. Move the declaration into your repository's own config if formwork should
own the wiring here, or chain formwork from the hook runner in charge, as the
message spells out. Issue #173.

The one refusal with an escape is a setting *wider* than this repository, such
as a machine-wide hook runner configured in your global git config. That is a
default rather than a decision this repository made, so you can override it here
and only here:

```sh
formwork hooks install --override-global   # sets core.hooksPath in THIS repository
```

It writes nothing outside this repository, and it does not unlock the refusals
above. `hooks verify` has no such flag — it only reports.

> **Fixed in 0.3.0.** On 0.2.0 and earlier the generated shim failed on every
> commit with `file-set modes require -C to be the repository root ... not .`,
> because the top-level guard could not accept the default relative root. It
> failed closed — commits were blocked, not silently unchecked — but the feature
> was unusable. If you hit that error, `formwork version` will tell you why.

**CI.** Two lines, plus the version pin:

```yaml
- run: formwork check --lane ci --format github
```

`--format github` emits workflow-command annotations, so findings appear inline
on the diff rather than buried in a log. Use `--format json` if you're feeding
another tool; never parse the human format, which is free to change.

**Only what changed.** For a fast PR gate:

```sh
formwork check --range origin/main..HEAD
formwork check --staged                    # what a pre-commit hook does
```

Whole-tree invariants (a required pattern that must exist *somewhere*, set
relations) still evaluate across the whole tree — narrowing the file set would
make those rules answer a different question and pass wrongly.

## 9. Keep the config honest

```sh
formwork lint
```

`check` audits your code. `lint` audits your **rules**, and it is the thing that
stops a guardrail suite quietly rotting into decoration.

If you have followed this guide literally, it will fail right now — twice — and
both failures are worth walking through, because they are the two ways a rule
suite dies:

```
[rules-present] OK
[prose-not-truncated] OK
[fixture-coverage] OK
[lane-reachability] OK
[lane-nonempty] FAIL — 1 problem(s)
  migrations: selects no rules
[empty-scope] OK
[exemption-hygiene] FAIL — 1 problem(s)
  no-print-debugging: scope.exclude "**/*_test.go" matches no files and has no justification comment
engine: never scans directories named .formwork, .git (any depth) — not an operator choice, and declarable in no rule's scope.exclude
escape hatches:
  no-print-debugging: scope.exclude: **/*_test.go
formwork lint: 5/7 checks passed
```

**`migrations: selects no rules`** — you copied the tag lane from §7 but have no
`sql`-tagged rule yet. A lane that selects nothing is not a harmless no-op: it
is a CI job that runs, passes, and checks *nothing*, which is worse than no job
at all because it reports green. Either tag a rule `sql` or drop the lane.

**`scope.exclude "**/*_test.go" matches no files`** — your repo has no test files
yet, so that exclusion protects nothing. Today it is harmless; the reason `lint`
cares is that the same message appears when a rule's exemption *outlives what it
was for*, and at that point the exclusion is quietly widening the rule's blind
spot with nobody watching. Either add the test file (the exclusion becomes real),
delete the exclusion, or justify it with a comment above the rule.

That is the whole design: **an escape hatch is allowed, but never silent.** Note
the `escape hatches:` block at the bottom — every exemption in the repo,
enumerated on every run, whether or not anything failed.

What each check catches:

| check | catches |
|---|---|
| `rules-present` | a config that loaded no rules at all |
| `prose-not-truncated` | a `cure:` YAML silently cut short — the field somebody reads when the hook blocks them |
| `fixture-coverage` | a rule with no fixtures — i.e. one nobody has proven works |
| `empty-scope` | a scope matching no files: a rule that *cannot* fire is not a passing rule |
| `lane-reachability` | a rule no lane ever runs |
| `lane-nonempty` | a lane that selects no rules |
| `exemption-hygiene` | dead exclusions, stale allowlist entries, unjustified escapes |

Run it in CI alongside `check`. A suite nobody audits becomes decoration within a
year — the rules stay in the file, they stop matching anything, and everyone goes
on believing they are protected. `lint` is what makes that visible while it is
still a one-line fix.

## Where to go next

- [`examples/quickstart/`](../examples/quickstart) — the five-rule worked
  example, commented rule by rule.
- `formwork explain <rule-id>` — renders any rule in full, from the binary.
- `formwork rules-for <path>...` — "what governs these files?", before you edit
  them.
- [VERSIONING.md](../VERSIONING.md) — what's API and what 0.x means.
- [CONTRIBUTING.md](../CONTRIBUTING.md) — if you want to change the engine
  itself.
