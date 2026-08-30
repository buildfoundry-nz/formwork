# CI/CD and versioned binary distribution — design

**Date**: 2026-07-23
**Status**: approved (brainstorm) — pending implementation plan
**Relates to**: `2026-07-09-formwork-design.md` §10 (distribution), §11 (error
handling), §12 (TDD strategy)

## Problem

formwork has no CI and no release mechanism. Nothing runs on push or PR; the
binary version is a hardcoded `const version = "0.1.0-dev"`
(`internal/cli/cli.go:40`); there are no git tags and no published binaries.

Consuming repos therefore *vendor* formwork. The validating consumer carries the whole
apparatus this implies: `tools/formwork/` (5,766 lines of vendored source),
`VENDOR_SHA.txt`, `VENDOR.sha256`, their formwork build script, and a
vendor-freshness gate that polices the checkout. That is the cost this design
removes: replace "copy the source and rebuild" with "install a pinned,
checksum-verified released binary."

The intended distribution story is already approved in §10 of the design spec
("released, versioned static binaries per OS/arch… adopting repos pin an exact
version… `formwork.yaml`'s engine-version constraint is the backstop — a
mismatched binary refuses to run"). This document realizes that paragraph. It
diverges from §10 in one respect, deliberately: §10 assumed GitHub-hosted
release assets, but this repo is **private and must stay private**, and GitHub's
cross-repo trust model makes private release assets unworkable as a consumer
channel (see "Why not GitHub Releases"). The consumer channel is **GCP Artifact
Registry**. §10 should be updated to match once this lands.

> **Superseded by the 2026-08-05 amendment below**: the channel is GitHub
> Releases after all. The paragraph above, "Why not GitHub Releases
> (private-only)", "Prerequisite infrastructure (one-time, GCP)", §2's
> WIF/`gcloud` steps, §7's consumer snippet, and Phase 2's GCP items are
> retained unedited as the decision record, not as the current design.

## Amendment (2026-08-05): the channel is GitHub Releases, not GCP AR

Two premises of the AR decision changed:

1. **"Private and must stay private" no longer holds.** The project intends to
   go open source, at which point GitHub release assets are exactly the
   publicly consumable channel §10 of the design spec originally assumed.
   Building the GCP path would provision infrastructure (AR repo, WIF pool,
   IAM bindings) that going public then obsoletes wholesale.
2. **The consumers who exist today are not cross-repo CI.** The immediate need
   is standalone-binary validation against the validating target by maintainers and peers
   who hold read access to *this* repo. Both objections in "Why not GitHub
   Releases" are cross-repo mechanics — a consumer repo's `GITHUB_TOKEN`
   scope, and composite-action resolution — and neither applies to a person
   running `gh release download -R buildfoundry-nz/formwork` with their own
   authenticated `gh`. Cross-repo CI consumption stays out of scope until the
   repo is public, at which point it requires no trust setup at all.

What §2 becomes under this amendment:

- `release.yml` on `v*` tags: first a `make verify` gate job on the same
  two-OS matrix that defines CI-green in `ci.yml` — nothing else enforces
  that a tag points at a CI-green commit, and an ubuntu-only gate would
  publish darwin tarballs behind a green run despite a darwin-only failure —
  then GoReleaser publishes the GitHub Release itself: `goreleaser release
  --clean`, `permissions: contents: write`, the repo-scoped `GITHUB_TOKEN`,
  no external secrets.
- Before publishing, the release job builds the current-platform target
  (`goreleaser build --single-target`) and runs
  `.github/scripts/assert-trusted-stamp.sh` — the same load-bearing
  stamped-and-trusted assertion `ci.yml`'s `release-config` job runs on every
  PR, extracted to one shared script so the two workflows cannot drift. A
  binary that would gate as untrusted is never published. The PR-time run
  covers the logic on snapshot versions; this run covers the real tag-derived
  version, the only place the tag-stripped-"v" regression class (2026-07-23)
  can manifest.
- `.goreleaser.yaml` drops `release.disable: true` (with `prerelease: auto`,
  so `-rc`/`-beta` tags mark themselves); the build config is otherwise
  unchanged.
- Consumers pull with `gh release download -R buildfoundry-nz/formwork
  --pattern 'formwork_*_<os>_<arch>.tar.gz'` and verify the archive against
  `checksums.txt` from the same release.
- No GCP provisioning ever happened, so nothing is torn down. `install.sh`
  and the WIF consumer snippet are not built.

Unchanged: tags are cut by hand (non-goal: no auto-versioning); the
engine-version backstop; the module path; Phase 1's CI exactly as shipped.

## Goals

- Continuous integration: `make verify` runs on every PR and push on each OS we
  ship for (linux + darwin), with every shipped target compile-checked.
- Tag-driven releases: a `v*` tag produces static binaries per OS/arch with
  checksums, published to a **GCP Artifact Registry generic repository**.
- A binary that reports its real version, stamped at release time.
- An engine-version backstop so a pinned — or *unidentifiable* — binary refuses
  to run rather than evaluating with different or unknowable semantics.
- A pinned, **keyless** install path for the two consumer environments that
  matter: **CI (GitHub Actions)** via Workload Identity Federation and **local
  dev machines** via `gcloud`/ADC.

## Non-goals

- `go install` as a primary path. The module-path fix below unblocks it, but no
  tooling is built around it and it is not tested as a consumption route.
- Windows binaries. Not a target environment.
- mise/asdf tooling. Not in this iteration.
- `release-please`/conventional-commit auto-versioning. Tags are cut by hand.
- A cross-repo composite GitHub Action. Dropped in favour of a ~4-line direct
  `auth` + `gcloud` snippet — see "Why not a composite action".
- Making the repo or its releases public. Ruled out (private-only is a hard
  constraint).
- Editing the consuming repo. Deleting its vendored tree is the *payoff* of this work but
  lands in that repo, later, and never from this one (CLAUDE.md).

## Why not GitHub Releases (private-only)

Two independent GitHub mechanisms each break the "thin install path" when the
producing repo is private and consumers live in *other* repos:

1. **Cross-repo token scope.** A consumer repo's built-in `GITHUB_TOKEN` is
   scoped to that repo; it cannot read another private repo's release assets
   (`gh release download -R buildfoundry-nz/formwork` 404s). The workaround is a
   PAT or GitHub App token provisioned and rotated into *every* consumer — long-
   lived secret, rotation burden, blast radius = read of formwork source.
2. **Composite-action resolution.** Even before any download, GitHub refuses to
   *resolve* an action that lives in a private repo referenced from another repo
   unless formwork's Settings → Actions → Access is opened to the org — an
   unmentioned, load-bearing provisioning step.

GCP Artifact Registry + Workload Identity Federation avoids both: auth is
keyless (no secret to rotate), IAM is centralized on one AR repo, and there is
no cross-repo GitHub trust to configure.

## Prerequisite infrastructure (one-time, GCP) — Phase 2

This is provisioning, not code, and is a **dependency** of the release upload
(§2) and every consumer pull (§7). It is entirely **Phase 2** (see Delivery
phasing): Phase 1 touches none of it. It needs a GCP project admin:

- A **generic** Artifact Registry repository (e.g. `formwork` in a chosen
  location, say `us`).
- A **Workload Identity Federation** pool + provider trusting this GitHub repo,
  so the release workflow authenticates keylessly to push.
- A **service account** with `roles/artifactregistry.writer` on the repo, bound
  to that WIF provider (release-time push).
- Consumer identities granted `roles/artifactregistry.reader` on the repo:
  each consumer repo's own WIF provider→SA for CI, and developers' own IAM for
  local `gcloud`/ADC pulls.

The exact project, location, and identifiers are recorded in the docs (§8) at
implementation time; the plan treats this provisioning as step 0.

## Decisions taken during brainstorm

- **Consumers**: CI (GitHub Actions) and local dev machines — both pull a pinned
  binary without building from source.
- **Access model**: private-only is a hard constraint; public is off the table.
  Distribution is keyless via GCP Artifact Registry + WIF (CI) / ADC (local).
- **Release tooling**: GoReleaser builds/archives/checksums on tag push; a
  follow-on step uploads the artifacts to Artifact Registry.
- **Install surface this iteration**: a `gcloud`-based `install.sh` (local +
  simple CI) and a documented ~4-line consumer CI snippet. **No composite
  action.**
- **Adjacent scope folded in**: engine-version backstop, and the module-path
  rename.
- **Release build**: all four artifacts (linux/darwin × amd64/arm64)
  cross-compiled by GoReleaser from a single runner — `CGO_ENABLED=0` static
  binaries need no native host per target. No Windows.
- **CI test matrix**: two *run* legs, one per platform — linux/amd64 and
  darwin/arm64. Arch rarely changes pure-Go behavior, so the two arch-crossing
  legs are dropped; all four targets still get *compile* coverage every PR via
  the `release-config` snapshot build.

  **The legs are named by PLATFORM and the runner label is separate** (#183,
  amendment 2026-08-24). Linux runs on `blacksmith-4vcpu-ubuntu-2404`; darwin
  stays on GitHub-hosted `macos-latest` because no Blacksmith macOS runner
  exists. Two things this shape protects, both learned rather than designed:

  - **The two legs are the definition of CI-green and neither may be dropped.**
    GoReleaser publishes darwin tarballs, so a darwin-only failure must
    red-gate them rather than ship behind a linux green. Collapsing onto one
    runner is the fail-open that `release.yml`'s verify comment exists to
    prevent.
  - **A check name must not encode a runner.** `verify (${{ matrix.os }})`
    tied the required-check names to the machine, so moving vendor renamed the
    checks branch protection pins (#117/#207). Naming by platform decouples
    them, and this is why the earlier vendor move (#181) moved only the two
    cheap jobs and left the expensive ones behind.
- **Engine-backstop fail-closed** (revised after review): an unstamped binary
  with `engine:` present **exits 2** by default — unidentifiable semantics must
  not read as a pass. Local iteration is unblocked by an explicit opt-in
  (`FORMWORK_ALLOW_DEV=1`), never silently.
- **Grounding** (verified 2026-07-23 against GitHub / GoReleaser / GCP docs;
  perishable — re-derive before implementing): `ubuntu-latest` = Ubuntu 24.04
  x86_64; `macos-latest` = macOS 15 arm64. GoReleaser v2 uses `archives.formats:`
  (list); singular `format:` is deprecated. AR generic repos host raw per-file
  artifacts via `gcloud artifacts generic upload/download` (package + version +
  name). `google-github-actions/auth@v3` performs keyless WIF and sets ADC, so a
  following `gcloud` works with no separate credential (gcloud itself is
  preinstalled on GitHub-hosted runners).

## Design

### 1. CI workflow — `.github/workflows/ci.yml`

Triggers: `pull_request` and `push` to `main`.

A `verify` job, matrixed across two runners — one *run* leg per OS:

| OS / arch      | runner          |
|----------------|-----------------|
| linux / x86_64 | `ubuntu-latest` |
| darwin / arm64 | `macos-latest`  |

Steps: checkout → `actions/setup-go` pinned to the `toolchain` in `go.mod`
(1.26.4) with build/module caching → `make verify` (test-with-race, vet,
fmt-check, check, selftest, lint). Using the Makefile targets keeps CI and local
byte-identical and inherits the `go list` filtering the Makefile already does.
`projects/` is absent in CI, so the bare-`./...` hazard never arises — but we do
not rely on that; we call the same targets a developer runs.

Why two legs and not four: arch (amd64 vs arm64) does not change pure-Go,
CGO-free behavior, so *running* the suite on both arches buys near-nothing; OS
(linux vs darwin — case-folding filesystem, path/permission/git-fixture
behavior) can, so each OS gets a leg. All four *release* targets still get
**compile** coverage on every PR through the `release-config` job below.

A second job, `release-config`, runs `goreleaser check` and `goreleaser build
--snapshot --clean` (no publish, no secrets — snapshot synthesizes a version, so
it needs no tag and runs on forked PRs) on `ubuntu-latest`. Because the release
build cross-compiles all four targets, this fails a PR that breaks the release
config *or* any target's compile — the arm/other-OS build coverage the trimmed
test matrix drops, recovered here for free.

### 2. Release workflow — `.github/workflows/release.yml` + `.goreleaser.yaml`

Trigger: `push` on tags matching `v*`. Permissions: `contents: read`,
`id-token: write` (for WIF).

One job:

1. checkout (`fetch-depth: 0`, so GoReleaser sees the tag) → setup-go.
2. `goreleaser release --clean --skip=publish` — build, archive, and checksum
   the four artifacts into `dist/`, **without** creating a GitHub Release
   (the consumer channel is AR, not GitHub). Optionally, a GitHub Release may
   still be cut as a maintainer-facing changelog record — it is never the
   consumer download source.
3. `google-github-actions/auth@v3` (WIF) → keyless ADC for GCP.
4. For each archive in `dist/` plus `checksums.txt`:
   `gcloud artifacts generic upload --project=<P> --location=<L>
   --repository=formwork --package=formwork --version=${GITHUB_REF_NAME}
   --source=<file>`.

`.goreleaser.yaml`:

- `builds`: one build with `main: ./cmd/formwork`, `binary: formwork`,
  `env: [CGO_ENABLED=0]`, `goos: [linux, darwin]`, `goarch: [amd64, arm64]`
  (4 artifacts), and `ldflags` injecting the version into the `internal/cli`
  package (see §3), not GoReleaser's default `main.version`:
  `-s -w -X github.com/buildfoundry-nz/formwork/internal/cli.version={{.Version}}`.
- `archives`: GoReleaser **v2** schema — `formats: [tar.gz]` (plural list key;
  singular `format:` is deprecated), `name_template:
  formwork_{{.Version}}_{{.Os}}_{{.Arch}}`.
- `checksum`: `checksums.txt` (sha256), uploaded to AR alongside the archives.

Deferred-but-cheap-later: SBOM and cosign attestation are left out now.
`checksums.txt` (plus AR's own stored digests) is the integrity primitive
install tooling verifies.

### 3. Version stamping

Replace `const version = "0.1.0-dev"` (`internal/cli`) with a package
`var version = "dev"` overridable by `-ldflags "-X
github.com/buildfoundry-nz/formwork/internal/cli.version=<tag>"` (the same
package path GoReleaser targets in §2).

Resolving that into an answer splits into two questions, deliberately kept
separate: **what version am I** (what `formwork version` prints) and **what
version do I trust for gating** (the input to the engine backstop in §4). A
binary can legitimately answer the first with a real, informative string —
useful in a bug report — while answering the second with `dev`, because the
string does not identify a released commit.

**Raw version** — the binary's self-report, with no judgement about release
status: the ldflags stamp if set and not `"dev"`, else
`debug.ReadBuildInfo().Main.Version` if present and not `(devel)`, else the
`"dev"` sentinel.

**Trusted version** — the raw version if it identifies a genuine release, else
`"dev"`. A version is a genuine release only if it is non-empty, not `dev`,
not `(devel)`, parses as semver, and is none of: a dirty working tree (`-dirty`
anywhere as its own component — e.g. `-dirty`, `+dirty`, or followed by
further build metadata such as `-dirty+build.7` — both the `git describe
--dirty` and semver-metadata spellings are rejected, at any position), a `git
describe` "commit ahead of its tag" string (`-<count>-g<hash>`, hash 4+ hex
digits — git's abbreviation floor — which names a commit, not a release), or
any of the three Go pseudo-version forms (`vX.0.0-<ts>-<sha>`,
`vX.Y.Z-0.<ts>-<sha>`, `vX.Y.Z-pre.0.<ts>-<sha>`). This validation applies
identically to the ldflags-stamped value and the build-info value — the
delivery path a version string arrived by must not change whether it is
trusted.

A leading `v` is **not** required, and must not be — a release version may or
may not carry one. `.goreleaser.yaml`'s `ldflags` stamps GoReleaser's `{{
.Version }}` template value (§2), which is the tag with its leading `v`
stripped: tag `v0.2.0` stamps the literal string `0.2.0`. Requiring a `v`
prefix here rejects every officially released binary — this shipped as a
release-breaking regression once (2026-07-23) precisely because no test bound
the engine-gate path to a non-`dev` version; semver validity, not a prefix
convention, is what makes a version trustworthy, and Masterminds semver
accepts both forms.

`formwork version` prints the raw version, annotated `" (unreleased build)"`
when it is not a genuine release (the bare `dev` sentinel is left
unannotated) — so a stamped-but-unreleased binary still identifies itself in a
bug report instead of erasing its version. The engine-version backstop (§4)
consumes the trusted version instead; the `dev` sentinel it may resolve to is
what makes §4's fail-closed branch fire.

### 4. Engine-version backstop (spec §10)

New **optional** top-level field in `formwork.yaml`:

```yaml
engine: ">=0.2.0"   # a semver constraint, github.com/Masterminds/semver/v3
```

Strict decoding is preserved — the field is added to the known set; an unknown
field is still an error.

Enforcement runs once, early, in every command that loads config — `check`,
`test`, `lint`, `scope`, and `hooks` alike — not only the ones that evaluate
rules. `lint` and `test` are gated deliberately, not incidentally (issue #17):
although they are introspection commands, their checks operate on *loaded* rules
(fixture coverage, escape-hatch enumeration, fixtures), and the gate runs before
`LoadRules` precisely so a too-old binary gets the clean version message instead
of an unknown-rule-type parse error. Carving them out was considered and
rejected — for the newer config that motivates the request, `LoadRules` would
fail on unsupported schema anyway, so a carve-out would trade the actionable
"install ≥ X" message for a confusing parse failure while still surfacing no
diagnostics (there is no loaded config to introspect). Critically, it runs
**after the `formwork.yaml` envelope is parsed but
before any rule file is read**: the CLI reads and parses `.formwork/formwork.yaml`
exactly once (`config.ReadEnvelope`), gates on that parsed envelope, and only
then compiles the full config (rule files included) via `Envelope.LoadRules`,
from the same envelope value the gate just evaluated — never a second,
possibly-different read of the file. (`config.Load(repoRoot)` remains a thin
`ReadEnvelope` + `LoadRules` wrapper for callers with no gate to run in
between.) Reading the file exactly once is deliberate, not just an
optimization: two separate reads (an earlier version of this design read the
envelope for the gate and then called a second, independent full-config load)
would let the gated bytes and the executed bytes differ if the file changed
between the two reads, weakening the gate's guarantee from "allowed to
evaluate THIS config" to "was allowed to evaluate some recent config". This
ordering also matters because a binary too old to understand a rule type
declared in a newer config would otherwise hit that unknown-type error first —
reporting a broken rule file when the real problem is an unsupported binary.
Gating on the envelope alone means the version mismatch is always diagnosed
correctly, regardless of what the rule files contain or whether this binary
can parse them:

- Field **absent** → no check (backward compatible; existing `version: 1`-only
  configs are unaffected).
- Field present, binary version **satisfies** the constraint → proceed.
- Field present, binary version **does not satisfy** → **exit 2** with
  `formwork: <v> does not satisfy engine constraint "<c>" declared in
  formwork.yaml` (the `formwork: ` prefix is the CLI's standard stderr prefix;
  the message body does not repeat "formwork"). A prerelease binary (e.g.
  `v1.0.0-rc.1`, or a `--snapshot` build) does **not** satisfy a plain
  constraint like `>=0.2.0` — Masterminds semver excludes prereleases from
  plain constraints by design, and this is deliberate and fail-closed here
  too; a constraint author who wants prereleases admitted must write
  `>=0.2.0-0`.
- Field present, binary's **trusted version resolves to `dev`** (§3) → **exit
  2 by default** with a message naming the situation: an unidentifiable engine
  must not silently skip its own constraint, or the backstop gives *false*
  assurance exactly when the binary is unknowable (the §11 "misconfiguration
  must never read as a pass" contract, one level up — the engine, not a rule).
  **Opt-out for local iteration only:** `FORMWORK_ALLOW_DEV=1` (env)
  downgrades this to a stderr warning + proceed. The opt-out is never set in
  the shipped CI snippet or install path, so a mis-installed/from-source
  binary in a consumer's CI fails closed rather than passing.
  `FORMWORK_ALLOW_DEV` is parsed as a boolean (`strconv.ParseBool`): `1`/`true`
  enable the opt-out; `0`/`false`/anything unparseable (including unset)
  leaves the backstop enforcing — an operator writing
  `FORMWORK_ALLOW_DEV=false` must get the fail-closed default, not an
  accidental opt-out.

  **The opt-out's real scope**: `FORMWORK_ALLOW_DEV` exists for binaries whose
  version *cannot be identified* — it is not license to discard a version we
  *can* identify as failing. The gate is given both the raw and the trusted
  version (not trusted alone) precisely so it can tell the two cases apart. A
  trusted version resolving to `dev` still carries a raw self-report that may
  be a stamped-but-unreleased string — any ldflags value the
  dirty/describe/pseudo-version rules above reject (e.g. a `-dirty` or `git
  describe` stamp). If that raw version parses as semver and demonstrably
  fails the constraint, the run is refused **unconditionally — even with
  `FORMWORK_ALLOW_DEV=1` set** — naming the raw version in the error. Only when
  the raw version can't even be evaluated (also `dev`, or not valid semver)
  does the opt-out's warn-and-proceed apply.

  A `--snapshot`/RC build is *not* one of these trusted-`dev` cases: a default
  GoReleaser snapshot stamp (`0.2.1-SNAPSHOT-<sha>`) and a `v1.0.0-rc.1` tag
  are genuine prerelease releases, so their *trusted* version is kept (§3),
  and they are refused on the trusted path by the plain-constraint prerelease
  exclusion above — never via this raw-version fallback. `FORMWORK_ALLOW_DEV`
  applies only when the trusted version is `dev`, so it cannot wave such a
  build through a plain constraint either; that is intentional, not a gap.
- Constraint string unparseable → exit 2 (config error), naming the field.

An opt-out that actively disables the backstop — `engine:` configured *and*
`FORMWORK_ALLOW_DEV` parsing as true *and the opt-out is actually in effect on
the running binary* — is itself an escape hatch, and `formwork lint`'s
escape-hatch enumeration (spec §9) says so: a line reading `engine-version
backstop: DISABLED via FORMWORK_ALLOW_DEV=<value>` appears alongside the
per-rule exemptions. The third condition is load-bearing: the opt-out only
ever does anything on a binary whose *trusted* gate version is the `dev`
sentinel (§3) — on a genuine released binary, `FORMWORK_ALLOW_DEV` is
completely inert regardless of how it's set, because the gate never reaches
the branch that consults it. Checking only "`engine:` configured and the env
var parses true" (as an earlier version of this design did) makes the
enumeration lie on a released binary: an operator's shell might have
`FORMWORK_ALLOW_DEV=1` exported for unrelated reasons, and lint would report
the backstop DISABLED even though it is, in fact, fully enforcing on that
binary — a false positive that is exactly as much a "nothing is silently
excluded" violation as a false negative would be. Because the fact this
depends on (the binary's trusted gate version) lives in package `cli`, which
`internal/meta` cannot import without a cycle, `cli` computes it and passes it
into `meta.Lint` as a plain `bool` rather than `meta` importing `cli` back.
The line is omitted when no `engine:` constraint is configured (the opt-out is
inert), when the variable is unset/falsy/unparseable (the backstop is still
enforcing), or when it parses true but the binary is a trusted release (the
opt-out would not engage) — consistent with "escape hatches stay visible;
nothing is silently excluded," now in both directions.

### 5. Module-path fix

Rename the module `github.com/buildfoundry/formwork` →
`github.com/buildfoundry-nz/formwork` (matching the actual remote
`github.com:buildfoundry-nz/formwork`). Mechanical sweep of `go.mod` and every
import. Lands as its **own atomic commit** so the rename diff is reviewable in
isolation from behavioral change. This also makes the `-X …/internal/cli.version`
ldflags path (§2/§3) correct.

### 6. `install.sh`

Repo-root script, `gcloud`-based (local dev + simple CI). Contract:

- **Required** version argument (e.g. `v0.2.0`) — no floating `latest`; pinning
  is the point.
- Optional install directory (default `./bin` or `$FORMWORK_INSTALL_DIR`);
  installs there and prints the path — it does **not** modify the caller's PATH.
- Detects OS (`darwin`/`linux`) and arch (`amd64`/`arm64`), maps to the archive
  name `formwork_<version>_<os>_<arch>.tar.gz`.
- Downloads the archive **and** `checksums.txt` via `gcloud artifacts generic
  download --project=<P> --location=<L> --repository=formwork --package=formwork
  --version=<version> --name=<file> --destination=<tmp>`. Requires `gcloud`
  present and ADC configured (local: `gcloud auth application-default login`
  once; CI: the WIF `auth` step); requires `roles/artifactregistry.reader`.
- Verifies the archive's sha256 against `checksums.txt`; mismatch → non-zero
  exit, no install.
- Extracts and installs the binary, `chmod +x`.
- `shellcheck`-clean; unsupported OS/arch, missing `gcloud`, and missing-auth
  cases fail loudly with actionable messages.

### 7. Consumer CI usage (no composite action)

Consumers add ~4 lines directly — no cross-repo action to resolve, no org
Actions-sharing setting, no cross-repo GitHub token:

```yaml
permissions:
  id-token: write        # for keyless WIF
  contents: read
steps:
  - uses: google-github-actions/auth@v3
    with:
      workload_identity_provider: <consumer's WIF provider>
      service_account: <consumer's SA with artifactregistry.reader on formwork>
  - run: |
      gcloud artifacts generic download --project=<P> --location=<L> \
        --repository=formwork --package=formwork --version=v0.2.0 \
        --name=formwork_v0.2.0_linux_amd64.tar.gz --destination=.
      tar xzf formwork_v0.2.0_linux_amd64.tar.gz
      install -m 0755 formwork /usr/local/bin/formwork
```

Consumers may instead call `install.sh <version>` after the `auth` step to get
the checksum-verify step for free. Optional caching via `actions/cache` keyed on
`formwork-<version>-<os>-<arch>-<sha256>` — the **checksum is in the key** so a
re-cut tag can never serve a stale binary.

This repo hosts and tests one canonical copy of that snippet (§ Testing) so it
does not rot.

### 8. Docs + downstream note

A "Distribution & consumption" section (README or `docs/`): the one-time infra
identifiers (AR project/location/repo, the WIF provider/SA naming pattern,
required IAM roles), the consumer CI snippet (§7), the local flow
(`gcloud auth application-default login` then `install.sh <version>`), and the
release procedure (`git tag vX && git push --tags`).

A downstream note records — without acting on it — that this work is what lets
the validating target delete `tools/formwork/`, `VENDOR_SHA.txt`, `VENDOR.sha256`,
their formwork build script, and its vendor-freshness gate, replacing them with
the WIF pull snippet + a pinned `engine:` constraint. That migration is a
downstream change, tracked separately, never made from this repo.

## Testing & TDD strategy

Per repo non-negotiables (test-first, exit-code contract, strict decoding,
determinism):

- **Version resolution** (§3): table tests over injectable seams for both
  halves of the split — raw resolution (ldflags wins; `dev`/empty ldflags
  fall through to build info; `(devel)`/empty build info → `dev`; no build
  info → `dev`) and trust validation (plain and `+incompatible`/prerelease
  semver trusted, **with or without a leading `v`** — the no-`v` GoReleaser
  `{{ .Version }}` form is an explicit regression-locked case, not merely
  tolerated; all three pseudo-version forms, `-dirty`/`+dirty` (including
  followed by further build metadata, e.g. `-dirty+build.7`), and `git
  describe` "-N-g&lt;hash&gt;" forms (hash 4+ hex digits) → `dev`), plus the
  display annotation applied on top of raw. A further table test binds the
  engine-gate seam itself (`gateVersionFrom`, the pure form of `gateVersion`)
  directly to non-`dev` inputs including the GoReleaser stamp form — the gap
  that let the leading-`v` regression ship: the rest of the suite tested raw
  resolution and trust validation in isolation, but nothing had ever pinned
  `gateVersion`'s own output on a real release string, so reverting it to
  `rawVersion` (dropping trust validation entirely) stayed green.
- **Engine backstop** (§4): table tests — absent field (proceed), satisfied
  (proceed), unsatisfied (**exit 2** + message), unstamped `dev` **with
  `FORMWORK_ALLOW_DEV` unset** (**exit 2** — the fail-closed case), unstamped
  `dev` with `FORMWORK_ALLOW_DEV=1` (warn + proceed), unparseable constraint
  (exit 2). The pure `checkEngine` seam takes both the raw and the trusted
  version (not trusted alone), so the table also covers the opt-out's real
  scope: a trusted-`dev` case whose *raw* version parses as semver and
  demonstrably fails the constraint is refused even with `FORMWORK_ALLOW_DEV`
  set, versus a trusted-`dev` case with no such evidence (raw also `dev`, or
  unparseable) where the opt-out governs normally. Config-decoding test
  confirms `engine:` is accepted and a still-unknown sibling field still
  errors.
- **install.sh** (§6): `shellcheck` in CI; a smoke test that points the script
  at a controlled artifact (a `--snapshot` build uploaded to a test AR
  package, or a local fake honoring the same layout) and asserts a verified
  install plus a checksum-mismatch rejection.
- **Consumer pull path** (§7): a CI job in *this* repo that runs the canonical
  snippet (WIF `auth` → `gcloud generic download` of a just-uploaded snapshot →
  `formwork version`), proving the keyless pull end-to-end and guarding the
  documented snippet against rot.
- **Release config** (§2): `goreleaser check` + `--snapshot` build gate every
  PR (the `release-config` CI job), so config breakage never waits for a tag.
- **CI workflow itself** (§1): validated by running; the two-leg matrix is the
  test of cross-platform `make verify`.

## Delivery phasing

Two independently shippable phases. **Phase 1 (build + test CI)** is the
priority and carries **no GCP dependency, no secrets, and no publishing** — get
every push/PR building and testing acceptably first. **Phase 2 (publish +
distribute)** adds the GCP infra and the release/consumption path, and begins
only once Phase 1 is green and accepted.

### Phase 1 — CI: validation & testing (no GCP, no secrets, no publish)

Each step lands test-first and shippable:

1. Module-path rename (atomic, mechanical).
2. Version stamping (`var version` + build-info/pseudo-version handling + table
   test).
3. `.goreleaser.yaml` — the build/archive/checksum config **only** (no publish,
   no upload). It exists so the `release-config` job can validate it.
4. CI workflow `ci.yml`: the two-leg `verify` matrix (§1) + the `release-config`
   job (`goreleaser check` + `goreleaser build --snapshot`, §1). Uses no secrets
   and no external services.

**Done when:** every PR/push runs `make verify` green on `ubuntu-latest` +
`macos-latest` and compile-checks all four release targets, with the release
*config* validated — but nothing is published anywhere.

### Phase 2 — publish & distribute (GCP infra + release + consumption)

Begun only once Phase 1 is accepted:

5. **Prerequisite infra** (GCP admin): AR generic repo, WIF pool/provider, push
   SA (`artifactregistry.writer`), reader IAM for consumers/devs.
6. Engine-version backstop (§4: config field + fail-closed runtime check +
   tests). Pure code with no infra dependency — it lives here because its value
   is realized only once pinned binaries are distributed and consumed, but it
   can be pulled into Phase 1 if convenient.
7. `release.yml` (build via the Phase-1 `.goreleaser.yaml` + WIF auth + AR
   upload, §2); validated by a tag + a test upload.
8. `install.sh` (gcloud-based; + shellcheck + smoke job, §6).
9. Consumer pull snippet + its CI smoke job (§7).
10. Docs + downstream note (§8).

**Done when:** a `v*` tag lands checksum-verified binaries in AR and both
consumer paths (CI via WIF, local via ADC) install a pinned version, guarded by
the fail-closed engine backstop.

## Open decisions (recorded, not blocking)

- **GitHub Release as maintainer record**: whether to also cut a (private,
  maintainer-only) GitHub Release for changelog visibility, or keep the tag +
  AR as the only artifacts. Default: skip; add only if a human-readable
  changelog surface is wanted. Never the consumer channel either way.
- **WIF infra ownership**: step 0 needs a GCP project admin; the plan must name
  who provisions the pool/provider/SA/IAM and where the identifiers live. This
  is a scheduling dependency, not a design question.
- **SBOM / cosign attestation**: deferred; `checksums.txt` + AR digests are the
  integrity primitive for now. Additive later.
- **arm-native test coverage**: the trimmed matrix never *runs* the suite on
  amd64-darwin or arm64-linux (darwin/arm64 *is* one of the two run legs). If a
  genuinely arch-sensitive bug is ever suspected, add a temporary
  `ubuntu-24.04-arm` leg (available on the private-repo standard tier). Release
  artifacts always cross-compile regardless.

## Superseded

- Earlier drafts of this spec distributed via GitHub Releases with a
  `token`-based `install.sh` and a `setup-formwork` composite Action. Both were
  removed after review: private-only distribution makes the cross-repo token and
  the composite-action resolution setting load-bearing and painful (see "Why not
  GitHub Releases"). The mechanism is now GCP Artifact Registry + WIF.
