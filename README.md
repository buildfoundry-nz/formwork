# formwork

[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)

A generic guardrails engine for repositories: one Go binary that evaluates
lockdown rules declared in tracked YAML (`.formwork/`), replacing fleets of
shell "gate" scripts (grep/awk/perl checks wired into git hooks and CI).

## Why

Repos accumulate guardrail scripts — forbidden patterns, required anchors,
cross-file consistency checks — each one a separate process that re-walks the
tree, with its own quoting bugs and platform hazards. Formwork replaces them
with:

- **One engine, one scan.** All fast rules evaluate against a single shared
  filesystem scan; each file is read once.
- **Rules as reviewable config.** Guardrails are declared in YAML, tracked in
  the adopting repo, and strictly validated (unknown fields are errors).
- **Built-in self-testing.** Rules ship with fixtures (`formwork test`) and
  the system audits its own integrity (`formwork lint`) — escape hatches and
  exemptions are always enumerated, never silent.
- **Operator-tuned ignores.** If tooling creates checkouts or noisy trees
  inside your repo (agent-harness worktrees under `.claude/worktrees/`,
  vendored source, generated output), declare them once in `formwork.yaml` —
  `scan.ignore` takes doublestar globs, each with a mandatory `reason`, and
  `formwork lint` enumerates every entry with its live match count so nothing
  is hidden silently.
- **Portability.** A single static binary; no BSD-grep/bash-3.2 divergence.

The first validating target is a private production repo whose 533
`check-*.sh` gate scripts define the parity benchmark: v1 is done when all of
them run under formwork with equivalent firing behaviour.

## Install

Releases ship four static tarballs (linux/darwin x amd64/arm64) plus
`checksums.txt`. There is nothing to build and no runtime to install.

```sh
VERSION=0.3.0        # pick a release and write the number down
OS=$(uname -s | tr '[:upper:]' '[:lower:]')          # linux | darwin
ARCH=$(uname -m | sed 's/x86_64/amd64/; s/aarch64/arm64/')

gh release download "v${VERSION}" --repo buildfoundry-nz/formwork \
  --pattern "formwork_${VERSION}_${OS}_${ARCH}.tar.gz" \
  --pattern checksums.txt

shasum -a 256 -c checksums.txt --ignore-missing      # sha256sum -c on Linux
tar xzf "formwork_${VERSION}_${OS}_${ARCH}.tar.gz" formwork
```

**Pin the version and verify the checksum.** This binary is about to decide
whether your CI passes, and an unpinned gate that silently changes semantics is
worse than no gate. Then declare the same floor in `formwork.yaml` so a
mismatched binary refuses to run instead of evaluating with unknown semantics:

```yaml
engine: ">= 0.3.0"
```

[`docs/quickstart.md`](docs/quickstart.md) has the resolve-the-latest-release
variant and the reasoning.

## Usage

```sh
formwork check     # evaluate all rules; exit 0 pass, 1 violations, 2 error
formwork test      # run every rule against its fixtures
formwork lint      # self-integrity checks over the config itself
formwork rules-for <path>...   # which rules govern these paths
formwork explain <rule-id>     # one rule in full
formwork list rules|lanes|types|preprocessors
formwork version
```

This repo self-hosts: its own rules live in `.formwork/`.

The exit codes are the contract worth wiring correctly: `1` means your repo has
violations, `2` means the engine never got far enough to say. Collapsing them
(`formwork check || true`) discards the difference between "clean" and "never
ran".

## Getting started

[**docs/reference.md**](docs/reference.md) is the operator reference: every rule
type and its parameters, every preprocessor, the `formwork.yaml` schema, the
exemption grammar, and the exit-code contract. Its type and preprocessor lists
are checked against the binary's registries by a test, so it cannot fall behind
the engine silently.

[**docs/rule-authoring.md**](docs/rule-authoring.md) is the judgement layer above
the reference: which rules are worth writing, how a corpus rots, and for each
practice either the `formwork lint` check that enforces it or a plain statement
that it is judgement with no enforcer.

[**docs/quickstart.md**](docs/quickstart.md) walks from nothing to a working
guardrail — install, first rule, fixtures that prove it fires, a pre-commit
hook and a CI lane — in about twenty minutes.
[`examples/quickstart/`](examples/quickstart) is the finished version: five
rules, each commented to introduce one concept.

[`stdlib/generic/`](stdlib/generic) is the portable hygiene pack — Go/Dart/SQL
rules that are not product-specific. Opt in with `library: [generic]` in
`.formwork/formwork.yaml` (requires `engine: ">= 0.6.0"`). Local rules override
pack rules by id.

## Development

```sh
make test     # full suite with race detector
make build    # build ./formwork
make check    # self-host check of this repo
make verify   # the full gate CI runs: unit tests, vet, gofmt, the self-host
              # check, every rule against its fixtures, config self-integrity,
              # and the end-to-end proofs
```

`make help` lists every target with its one-line description. It reads the
Makefile, so unlike a list written out here it cannot fall behind what `verify`
actually depends on.

The approved design lives in
[`docs/specs/2026-07-09-formwork-design.md`](docs/specs/2026-07-09-formwork-design.md)
— read it before changing architecture, config schema, rule types, or the CLI
surface. Development is strictly test-first; the non-negotiables are in
[CONTRIBUTING.md](CONTRIBUTING.md).

## Project policies

- [**CONTRIBUTING.md**](CONTRIBUTING.md) — how work enters the repo, and the
  non-negotiables (test-first, the exit-code contract, the fail-open defect
  class, the fixture obligation).
- [**SECURITY.md**](SECURITY.md) — private vulnerability reporting, and the
  threat model. Worth reading before adopting: `command:` rules execute
  programs declared in tracked config, so `.formwork/` carries the same power
  as your CI workflow definitions.
- [**VERSIONING.md**](VERSIONING.md) — what counts as API (config schema, rule
  vocabulary, exit codes, JSON output) and what 0.x means for each. Pin the
  binary and declare `engine:`.

## Status

0.x — see [VERSIONING.md](VERSIONING.md). The engine and the exit-code contract
are stable; the rule-type vocabulary is still growing as real corpora demand
new types, and that is the surface most likely to move under you.

## License

Apache License 2.0 — see [`LICENSE`](LICENSE). Binary distributions embed
third-party work (notably libpg_query and its PostgreSQL-derived code, via
`wasilibs/go-pgquery`); attributions are in [`NOTICE`](NOTICE).
