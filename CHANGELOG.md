# Changelog

## 0.16.0

### Added

- **`formwork check-content --batch`: one corpus load for an NDJSON file set.**
  Each stdin line is a strict `{path, content}` object and each output line is
  the existing schema-1 result for that record, in input order. Reachability,
  generated state, findings, content hashes, and replay handles are independent
  per record; finalizers and cross-file rules stay deferred. Empty or malformed
  input fails closed, and the aggregate exit is 1 when any record has an
  error-severity finding. `--path` and `--batch` are mutually exclusive.
- **`formwork check-content --path P`: stateless single-content evaluation.** The
  command reads complete file bytes from stdin and runs only proven file-local
  rules through the existing engine. Its versioned JSON report carries corpus
  and content hashes, evaluated rule ids, applicable deferred reasons and
  out-of-scope counts, bounded findings with cures and explain handles, and a deterministic
  replay fingerprint. Zero eligible rules is distinct from eligible rules
  passing; ignored and nonregular paths are `not_scanned`. Whole-tree,
  finalizing, external-tool, census/credential, and tree-reading rules are
  deferred, and SQL analyzers opt in only for `.sql` paths. Full `check` is
  unchanged.
- **`FORMWORK_FILES`: a changeset run tells a detector which files changed.**
  Under `--staged` and `--range`, a command or census detector runs with
  `FORMWORK_FILES` naming a listing of the changed files its rule's scope
  admits. A detector that judges each file on its own can read that and parse
  nothing else. A whole-tree run sets nothing, so the detector judges the root
  as before. On a 4,300-rule corpus, nine Go census detectors re-parsed about
  22,700 Go files on every Go commit, roughly 170 CPU-seconds, to judge the
  handful of files the commit changed.
- **`dart/marked-single-home`: the Dart form of `go/marked-single-home`.** A
  top-level declaration whose comment run carries `params.marker` is the one
  declaration of its identity in scope; any second declaration of it is a
  finding naming the home. Declarations are column-0 lines outside strings and
  block comments, so members never count, and a library-private name is keyed
  on its file. On a 4,300-rule corpus, 48 Dart `pattern-count` rules each
  pinned one such name to `n: 1`, and each one's mutation proof planted a
  second declaration.

## 0.15.1

### Fixed

- **A release names its repository.** v0.14.3 and v0.15.0 were tagged but
  never published: the release runner fetched through a git mirror, whose URL
  rewrite (`url.<mirror>.insteadOf`) is what `git remote get-url` reports, and
  GoReleaser read the repository from that URL — owner `github.com` — so every
  GitHub API call 404'd. `.goreleaser.yaml` now names
  `buildfoundry-nz/formwork`. 0.15.1 is 0.15.0 (and so 0.14.3) published.

## 0.15.0

### Added

- **`tests:` and `scope.subject: production`.** A repository declares its test
  code once in `formwork.yaml` (`tests:`, path globs). A rule whose subject only
  production code can have says `scope.subject: production` and reads no file
  `tests:` names; every other rule reads tests. This replaces a per-rule
  `scope.exclude` of test globs, which could not be told from an exclude that
  forgives a violation. The escape-hatch census counts the test files each
  production subject leaves out, `formwork lint` refuses one that leaves out
  none, and a production subject with no `tests:` definition is refused at
  load.

### Changed

- **The walk reads the repository's `.formwork`, except its fixtures.** The
  root `.formwork` was pruned whole, so no rule could read `formwork.yaml`,
  the rule YAML or the mutation specs, and every check over the corpus itself
  had to shell out (`type: command`). Only the root's `.formwork/fixtures` is
  pruned now — each fixture is its own rule's proof, judged by `formwork
  test`. The rest is scanned, and reached only by an include glob rooted at
  `.formwork/`: `**` does not descend into it, so a rule written for
  application code never reads the definitions that spell its own token. A
  root `.formwork` symlink (a shared rule set) is still skipped, and now
  recorded as a link the walk declined to follow. A `scan.ignore` glob hiding
  tracked rule YAML is reported like any other hidden tracked path.
- **The SQL rules read SQL template functions.** A repository keeps a
  predicate in one place as a function of the aliases and placeholders it is
  spliced with — `VisibleWhere("p")`, `OverdueSQL("$1")` — and both its body
  and every query calling it were compositions no rule could read. A
  *template* — a package-level function whose parameters and result are all
  `string` and whose body is one `return` — is now read: a call with constant
  arguments reads as the instance it builds, at the call, and the definition
  is judged once per distinct argument set the walk calls it with. Its body is
  then not unread SQL. A template some call feeds a non-constant, or that
  nothing calls, stays unread at the definition, and the non-constant call is
  unread where it stands.
- **`sql/parses` and `sql/statement-predicate` fail closed.** A Go composition
  carrying SQL that the extractor cannot read — an operand that is a variable,
  a call or a field — is a finding, `SQL could not be read (…), so this rule
  cannot judge it`, where it was a census line and a pass. For
  `sql/statement-predicate` it is a finding whatever the `table` pattern says,
  since the unread operand may be the table. The escape-hatch census no longer
  lists these two types. **Migration:** compose such statements from Go
  constants and bind what varies as a parameter; `formwork lint` on the
  previous release lists every site.
- **The SQL rules read Go string constants.** A query composed from a named
  constant — `"SELECT " + cols + " FROM t"`, `scanvault.Cols + " WHERE …"`,
  `fmt.Sprintf("… %s …", cte)` — was a composition no SQL rule could read,
  so it was judged as fragments or not at all. Both extractors now read a Go
  constant expression of string type whole: literals, constants declared in
  the file, elsewhere in its package, or in an imported package of the module
  (located by `go.mod`, its module path and its directory `replace`s), `+`,
  conversions to a string type, and `fmt.Sprint*` calls of constants with
  plain `%s`/`%v` verbs. A name declared more than once in a package is not
  read. On one corpus this took the census from 1,909 unreadable SQL
  compositions to 1,025, and its SQL rules found 22 violations the fragments
  had hidden. A rule now reads a sibling file on a `--staged`/`--range` run
  even when only the composing file changed.
- **The escape-hatch census lists only SQL it could not read.** For
  `sql/parses` and `sql/statement-predicate` it listed every `+` chain and
  `fmt.Sprint*` call it could not fold as "SQL … could not be read", whatever
  the string held. One corpus's SQL rules were each told this about 22,693
  lines, 2,040 of which carried SQL; the rest were log messages, paths,
  regular expressions and e-mail text. A composition is now listed when its
  text carries a statement, a clause or a SQL-only token (a cast, a positional
  parameter, `IS NULL`); `internal/rules/sqlparse/sqlbearing.go` states the
  test and what it misses. A composition nested in another is listed once, as
  the whole.
- **The `generic` pack carries no generated, vendored, build or test
  excludes.** Its rules read every file the adopting repository does not
  declare as generated (`generated:`), test code for a production subject
  (`tests:`), or ignored (`scan.ignore`, `scan.gitignore`). The per-rule
  globs that stood in for those facts are gone, with the paths one adopter
  had pinned into a portable pack (`gcs/`, `blobstore/`, `internal/vendorjson/`,
  `internal/lockdowntests/`, `tools/formwork/`). Three pack rules state
  `scope.subject: production`, so an adopter loading `library: [generic]`
  declares `tests:`; without it the load names what is missing. The one
  exclude left is typed: markdown under `.husky/` is documentation, not a
  hook script.

## 0.14.3

### Fixed

- **A pruned tree runs beside a rule with homes.** A synthetic or scratch tree
  that carries one rule beside only the files its own proof needs holds none
  of the repository's not_declaring homes or credential holders. `formwork
  check` reported each as a holder count of zero, so a synthetic test could
  not run any rule that names a home outside its scratch tree. A count of zero
  is now `formwork lint`'s: lint reports a not_declaring marker no file
  declares (as before) and, new, a require_absent credential no in-scope line
  holds. A count above zero that differs from the declaration is still a
  `formwork check` finding.

### Performance

- **A credential's holder count skips files that cannot hold it.** 0.14
  counts every credential over every in-scope file, ahead of any prefilter,
  and the line scan was about a third of a credential rule's CPU. Each
  credential now carries the literal gate `autoPrefilter` already derives for
  a pattern: a file holding none of the credential's guaranteed literals holds
  none of its lines, so it is skipped without changing the count. On
  TakeoffQS's 243 home-carrying rules over the whole tree, CPU went from 128 s
  to 82 s.

## 0.14.2

### Fixed

- **A pruned corpus loads beside a `generated:` definition.** 0.14.0 refused
  to load a `formwork.yaml` whose `generated:` `proven_by` named a rule absent
  from the loaded corpus. Tools that load one rule and its fixtures beside the
  repository's `formwork.yaml` (a mutation-proof scratch tree, a benchmark
  harness, a synthetic test) therefore could not evaluate anything. The check
  now lives in `formwork lint`'s `generated-definition`, which reads the whole
  corpus: a proven_by that names no rule is a lint failure, not a load error.

## 0.14.1

### Added

- **`go/marked-single-home`: a home is declared at the declaration.** A
  top-level func, method, type, const or var whose doc comment carries
  `params.marker` is the one declaration of its identity in scope: a second
  declaration of the same kind and name (and receiver type, for a method),
  marked or not, is a finding that names the home. A marker that documents no
  top-level declaration is a finding too. It is the abstract form of the
  `pattern-count` rule-per-name family (`'^func offerFor\('`, `n: 1`): on a
  4,300-rule corpus, 67 of the 125 Go rules in that family have mutation proofs that plant exactly the
  second declaration this one rule catches.

## 0.14.0

### Changed (breaking)

- **A credential sanctions the declaration it opens, never the file.** A
  forbidden-pattern `require_absent`/`none_of` credential used to excuse the
  whole file that held it, so a second violation anywhere else in an owner's
  file was invisible. The engine now masks only the declaration the credential
  line opens (`internal/declspan`): in Go, the statement its first token begins
  (a func or method through its body, a const/var/type spec through its value;
  a `package` clause is the whole file), in Dart and proto the declaration
  through its first top-level block or semicolon, in non-code files the
  document. A credential on a comment line sanctions only that line. A file
  that holds a home also owns its import directives (Go `import`; Dart
  `import`/`export`/`part`/`library`), which serve every declaration in it and
  cannot sit inside one. Holders are counted as before. Migration: an owner file that also uses the pattern
  outside the owner declaration now fires there; move that use into the owner,
  or give it its own credential and raise `holders`.
- **`scope.not_declaring` entries are `{marker, holders}` and name a home
  declaration.** A bare-string entry is refused at load. Each place a marker
  occurs opens a home, the declaration that begins on that line (the same
  extents a credential uses); the engine masks it and judges the rest of the
  file, where it used to skip the whole file. `holders` pins the number of
  places in scope that declare the marker, judged on the live tree, and a rule
  declaring the key is a whole-tree invariant, so it runs over the tracked tree
  under `--staged`/`--range`. The census reports declarations removed rather
  than files.

### Added

- **One definition of generated output.** `formwork.yaml` `generated:` lists
  `{marker, proven_by}` entries. A file whose leading comment block carries a
  declared marker is outside every rule that does not declare
  `scope.generated: true`, replacing per-rule lists of generator output globs.
  `proven_by` is required and must name corpus rules: the freshness gates that
  prove a file carrying the header reproduces from its generator. `formwork
  lint`'s `generated-definition` check fails a proving rule no CI lane selects
  and a marker no scanned file carries, and the escape-hatch census counts each
  marker's files.
- `scan.File.Raw()` returns the source a preprocessor variant was computed
  from.

## 0.13.2

### Changed

- **A `.formwork/` change runs only what it can affect.** Under `--staged`
  and `--range`, any path under `.formwork/` used to run every
  changeset-gated rule. Now `formwork.yaml` still runs every rule, and an
  edited rule file runs the rules it declares. Fixtures, mutation specs and
  the durations hint run nothing extra, because `check` loads only
  `formwork.yaml` and `rules/*.yaml` and its walk never enters the
  directory. For the same reason, a path under a root `.formwork/` no longer
  reaches a scope-bound invariant whose globs happen to match it. A census
  detector reads the root itself, so an admitted path still runs it. Each rule
  now records its source file (`Rule.File`). On a 4,300-rule corpus a range
  touching one mutation spec took 494 s, against 33 s for one touching a
  docs file.

## 0.13.1

### Changed

- **forbidden-pattern skips files that cannot match.** A forbidden-pattern
  rule with no declared `prefilter` now derives one from its RE2 pattern:
  literals at least one of which every match contains, taken from the
  pattern's guaranteed runs. A file whose content holds none of them is not
  scanned line by line. The gate tests the same preprocessed variant the
  lines come from, so no verdict changes. It declines whenever it cannot
  prove that: regexp2, case-folded literals, an alternation branch with no
  guaranteed run, literals under 3 bytes, more than 8 branches, or U+FFFD
  (Go's regexp matches invalid UTF-8 as U+FFFD, and a byte search would miss
  it). On a 4,300-rule corpus a whole-tree `check --skip-escapes` went from
  273 s to 153 s wall and 1,365 to 996 CPU-seconds, with identical findings,
  and its fixture suite passed unchanged (13,894 fixtures).

## 0.13.0

### Removed (breaking)

- **No finding can be exempted.** The `except:` block is gone from the rule
  schema, so a rule declaring `except.paths` or `except.marker` is refused at
  load (exit 2, `field except not found`) instead of running with the excusal
  ignored. A `formwork:allow` comment is now ordinary text: the finding on its
  line fails the run like any other. Gone with them: `internal/marker`,
  `finding.Suppressed`/`SuppressedBy`, `finding.Unsuppressed` and
  `corpus.Unsuppressed`; the `suppressed` array and `summary.suppressed` in
  `-format json`, the `::notice` suppressed lines in `-format github` and the
  `N suppressed` summary tail; `exemption-hygiene`'s reasonless-marker arm; the
  census's `marker enabled`, `suppressed` and `except.paths` lines; `explain`'s
  `except_paths` and `marker` fields; and the vacuity census's DEAD-EXCEPT-GLOB
  and INERT-EXCEPT arms with their `# except-declaration:` vocabulary.
  `config.New` and `corpus.New` drop their except-paths argument.
  Migration: cure each excused file, or narrow `scope.include` to what the rule
  is about.
- **`sql/locking-select-order` names a recomposition, not a marker, as its cure** for
  the disclosed #42 and #337 worlds: compose the ORDER BY where this pass can
  read it on every path.

### Fixed

- **The escape-hatch census reports `scope.not_declaring`.** It printed nothing
  for the one scope subtraction keyed on file content, although its own
  documentation said it was reported with live match counts. Each marker now
  prints how many in-scope files it removed and how many of those the rule
  would have fired on. `explain` shows the markers too.
- **A dead `scope.not_declaring` marker beside a live sibling is reported.**
  `exemption-hygiene` asked whether any of a rule's markers matched a file,
  so one declared marker made every sibling read as live.

## 0.12.2

### Fixed

- **The engine's git calls no longer wait on core.fsmonitor.** git consults
  fsmonitor on every index read and waits for its answer, so a wedged
  fsmonitor daemon stalled `formwork check --staged` indefinitely. On one
  adopter machine that check held a machine-wide pre-commit lock for ten
  minutes and stopped every commit behind it. Both of internal/vcs's exec sites
  now run git with `-c core.fsmonitor=false`. fsmonitor only changes how git
  finds changed paths, not what it answers, so every answer is unchanged.
  The ignored-path listing was also faster without it: 8 s against 11 to 16 s
  on a 66,000-file worktree under load.

## 0.12.1

### Changed

- **Scope matching skips re-validating stored globs.** `Rule.Applies` runs for
  every (file, rule) pair the engine considers, and almost every pair is a
  miss. `doublestar.Match` re-validates the whole pattern on each miss, which
  was about a third of `check`'s in-process CPU on a 4,300-rule corpus.
  `scope.include` and `scope.exclude` are validated once by `config.New` and
  cannot be reassigned outside the package, so they now match unvalidated;
  `except.paths`, an exported field, keeps the validating matcher.
  `BenchmarkRuleAppliesMostlyMisses`: about 2.15 µs to 1.43 µs. Verdicts are
  unchanged.

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
