# Stdlib library packs — generic rules distributed with the engine

Status: implemented in this change.
Supersedes the unbuilt recommendation in `docs/parity/GENERIC-VS-SPECIFIC.md` §Recommended
config handling item (1): a pinned generic rule pack, merged by `Load()`, local
wins by id.

## Why

The engine is generic. Rule *instances* were repo-local only. The first
validating configuration (TakeoffQS) contains ~55 gates that would apply to any
Go/Dart/SQL repo (gofmt, weak types, no `skip:` in tests, immutable migrations,
no committed binaries, …) and ~188 more whose *logic* is generic but whose
globs were product-specific.

`formwork init` and `examples/quickstart/` shipped one-to-five teaching rules.
`examples/palletra-port-full/` shipped hundreds of product-domain rules under
fictional names. Neither is a pack a second repo can opt into.

## Shape

1. **Pack on disk**, tested as a corpus:
   `stdlib/generic/.formwork/{formwork.yaml,rules/,fixtures/}`
   `make selftest` / `make lint` loop it the same way they loop `examples/*/`.

2. **Pack in the binary**, via `go:embed` of `stdlib/generic/.formwork/rules/*.yaml`.
   Adopters do not copy fifty files. They declare:

   ```yaml
   # .formwork/formwork.yaml
   version: 1
   engine: ">= 0.6.0"
   library: [generic]
   ```

3. **`Load()` merge order.** Library rules load first. Local `.formwork/rules/*.yaml`
   load second. Duplicate id: local replaces the library rule (the mixed-binding
   hatch — keep the logic, rebind scope/allowlist by restating the rule). Duplicate
   id across two local files is still exit 2. Duplicate id across two library files
   is exit 2. Unknown pack name is exit 2 and lists the known packs.

4. **Library rules cannot carry `except.allowlist`.** That path is repo-local.
   To bind an allowlist, redeclare the rule locally (local wins). `except.paths`
   globs are allowed; they are not file reads.

5. **Fixture coverage.** `formwork lint` in an adopting repo does not demand
   `.formwork/fixtures/<id>/` for a rule whose `Library` field is set — those
   fixtures live in the pack and are proven by `formwork test -C stdlib/generic`.
   The pack corpus itself has no `library:` key; its rules are local to that tree
   and *do* require fixtures.

6. **Command-type detectors that `go run` a TakeoffQS script are not in this
   pack.** They are not portable without shipping the script. Declarative types
   (forbidden-pattern, required-pattern, pair-consistency, set-relation,
   file-naming, go/*, dart/*, …) are. Product-named CI wiring pins (`b06-*`)
   stay out.

## Non-goals (this change)

- `from: <template-id>` parameterized mixed templates (GENERIC-VS-SPECIFIC item 2).
- Auto-enabling the pack. Opt-in via `library:`.
- Replacing `examples/palletra-port-*`. Those remain scale corpora.
- `formwork init` (still unbuilt).
