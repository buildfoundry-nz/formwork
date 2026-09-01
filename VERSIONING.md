# Versioning and compatibility

formwork is at **0.x**. This document states what counts as its public
interface, what may change, and what protects you when it does.

## The short version

- Everything below marked **API** is something an adopting repo depends on and
  we treat as an interface, not an implementation detail.
- While at 0.x, a **minor** bump (`0.2.0` → `0.3.0`) may make a breaking change
  to any of it. A **patch** bump (`0.2.0` → `0.2.1`) will not.
- Every release notes its breaking changes explicitly.
- Your protection is the `engine:` constraint in `formwork.yaml` plus a pinned
  binary. Use both. See [Protecting yourself](#protecting-yourself).

## What is API

### Exit codes — the strongest guarantee here

| code | meaning |
|------|---------|
| `0`  | pass — every rule ran and none found a violation |
| `1`  | violations found |
| `2`  | engine or configuration error |

This contract will not change in 0.x, and it is the one thing we would rather
break a release than get wrong. The distinction that matters is `1` vs `2`: a
crashed, panicking, or misconfigured rule must **never** read as a pass. If you
find a path where it does, that is a security-adjacent bug — see
[SECURITY.md](SECURITY.md).

Do not collapse these in your CI wrapper. `formwork check || true` and
`if ! formwork check; then warn; fi` both discard the difference between "your
repo is clean" and "the engine never ran."

### Configuration schema — API, and strictly decoded

`formwork.yaml` and everything under `.formwork/` is API: the rule envelope
(`id`, `type`, `severity`, `scope`, `preprocess`, `cure`, `origin`, `tags`),
each rule type's `params`, lane declarations, and the exemption grammar.

**Config decoding is strict: an unknown field is exit 2, not a warning.** That
is deliberate — a typo'd key that silently disabled a guardrail would be the
worst kind of failure for this tool. But it has a consequence worth
understanding, because it is the reverse of what people usually expect:

> A config written for a **newer** engine fails hard on an **older** binary.
> Adding a field to the schema is therefore a breaking change *for anyone whose
> binary is behind*, even though nothing was removed.

This is why the `engine:` constraint exists and why it is checked before any
rule file is parsed — so you get a legible version error rather than a confusing
unknown-field error.

### Rule-type and preprocessor vocabulary — API

The set of registered `type:` strings and `preprocess:` values, and the params
each accepts. Removing or renaming either is breaking. Adding is subject to the
strict-decoding caveat above.

You never have to guess what a given binary supports:

```sh
formwork list types          # straight from the registry
formwork list preprocessors  # likewise
```

Both read the live registries rather than a hand-maintained list, so a pinned
binary's reported vocabulary cannot drift from what it actually implements.
This is the intended answer to version skew — ask the binary, don't consult a
table.

### Machine-readable output — API

- **`-format json`** is API. Field names and structure are stable within a
  minor version. Findings are sorted deterministically by (rule id, path, line,
  message), and that ordering is part of the contract — diffing two runs is a
  supported use.
- **`-format github`** emits GitHub Actions workflow-command annotations. The
  format is GitHub's, not ours; we track it.
- **`-format human` is NOT API.** It is for people, it will change to read
  better, and it should never be parsed. If you are grepping human output, you
  want `json`.

### CLI surface — API

Command names (`check`, `test`, `lint`, `scope`, `hooks`, `explain`, `list`,
`rules-for`, `version`) and their flags. Removing a command or flag, or changing
a flag's meaning, is breaking. Adding is not.

## What is NOT API

- Human-readable output text, including finding messages and `cure:` rendering.
- Log and diagnostic wording.
- Everything under `internal/` — this is a Go module, but the engine is
  consumed as a **binary**, not a library. There is no supported Go API surface,
  and `internal/` enforces that at the compiler level.
- Performance characteristics, worker-pool behavior, and scan ordering (as
  distinct from *output* ordering, which is API).
- The set of rules in this repository's own `.formwork/` — those are how
  formwork guards itself, not a template to depend on.

## Protecting yourself

Two mechanisms, and you want both:

**1. Pin the binary.** Wherever you install it — CI setup step, mise/asdf, a
hook shim — pin an exact version. Releases are static tarballs with a
`checksums.txt`; verify it.

**2. Declare `engine:` in `formwork.yaml`.** A semver constraint that says which
engine versions your config is written for:

```yaml
engine: ">=0.2.0 <0.3.0"
```

This is checked **before any rule file is parsed**, so a mismatch is a clear
version error instead of a cascade of unknown-field errors. It is also the
backstop for the case pinning cannot cover: a developer with a stale binary on
their PATH running the hook locally.

A binary that is not a real tagged release does not satisfy any `engine:`
constraint. That is intentional — an unidentified build cannot honestly claim
to meet a version requirement. A local dev build therefore needs an explicit
opt-out (`FORMWORK_ALLOW_DEV`) rather than silently passing the gate, and when
that opt-out is active the engine says so in its own output rather than letting
you forget.

## What 0.x means in practice

0.x is not a disclaimer here; it is a statement about a specific thing. The
engine internals and the exit-code contract are stable and well tested. What is
still moving is the **rule-type vocabulary** — new types get added as real
corpora demand them, and occasionally an existing type gains a param or has one
tightened. That is the surface most likely to move under you, and it is exactly
the surface `engine:` and `formwork list types` exist to make legible.

We will move to 1.0 when the rule-type vocabulary has been stable across
several real adoptions rather than one.

## Deprecation

When something API must go:

1. It is announced in the release notes of the version that deprecates it.
2. Where the engine can detect the deprecated usage, it warns — on stderr, and
   never by changing the exit code.
3. It is removed no earlier than the following minor version.

A change that cannot follow this — because strict decoding leaves no room for a
soft landing — is called out as breaking in the release notes, with the
migration written down.

## 0.5.0

**Breaking.** `formwork test`: a `.formwork/fixtures/<id>/` directory matching
no live rule id is a FAIL verdict (exit 1) rather than a repo-wide abort
(exit 2), when at least one rule is configured. Other rules still run. Zero
rules configured plus orphan dirs remains exit 2 and still names the dead
trees. Symlink refusals and unreadable dirs remain exit 2.

See [CHANGELOG.md](CHANGELOG.md).
