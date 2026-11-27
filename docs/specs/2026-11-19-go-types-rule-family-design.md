# `go/types` rule family, run as one vettool

Status: design. Consumer driver: a downstream epic #24306, Phase 2 (W2, "formwork
`go/types` rule family compiled into one vettool").

## 1. Problem

Every Go rule formwork evaluates today reads text or syntax. `forbidden-pattern`
matches bytes, and the `go/*` family (`internal/rules/goast`) walks one file's
`go/ast`. Neither knows what a name refers to. A rule that bans `any` in a
parameter cannot see:

- an alias declared in another package (`type Bag = any`, then `func F(b Bag)`);
- a type argument (`Box[any]`), a slice or map element (`[]any`, `map[string]any`);
- an inferred type (`x := decode()` where `decode` returns `any`);
- a parameter list wrapped across lines in a shape the pattern did not foresee.

The consumer's `go-weak-types-any` is a 500-character regex with an arm per
spelling it has met, and it still misses each shape above. The consumer has
been converting such rules to `type: command` programs that load `go/types`
themselves, which takes them out of the edit loop (`check-content` cannot run
a command rule) and gives each its own package loader.

## 2. Decisions

1. **The analyzers live in formwork and run under `go vet`.** formwork links
   `golang.org/x/tools/go/analysis` and its `unitchecker`. The go command
   already knows how to build each package's type information, honours build
   tags and cgo, and caches facts and results per package. formwork
   does not load packages itself.
2. **The formwork binary is the vettool.** When formwork runs `go vet`, it
   passes its own executable as `-vettool` and sets `FORMWORK_VETTOOL=1`.
   `main` sees the variable and hands control to `unitchecker.Main` before any
   CLI parsing. Nothing else is installed, and the analyzers can never be a
   different version from the engine that reads their output.
3. **One `go vet` per Go module per rule, and the go command's cache does the
   rest.** A rule's analyzer and params reach the vettool as one analyzer flag,
   `-formwork.plan=<json>`, never an environment variable: the go command keys
   its cached vet results on the vettool and the flags it passes, so a plan in
   the environment could be answered from a run made under another plan. Type
   information for dependencies comes from the build cache either way, so a
   second rule over the same packages pays for its own analysis, not for
   type-checking again. Batching every rule into one `go vet` needs state shared
   across checkers, which the engine does not have; it is phase 3, if the
   measured cost asks for it.
4. **Scope is the rule's, as everywhere.** formwork vets the packages that hold
   an in-scope file, grouped by the nearest `go.mod`, and keeps only
   diagnostics positioned in an in-scope file.
5. **A package that does not type-check fails the rule closed.** `go vet`
   reports it as an error, and the rule is an engine error rather than a pass.
   A type error is not evidence that the file is clean.
6. **Unsaved content is phase 2.** The candidate route is `go vet -overlay`,
   so the rule judges the package as it would be after the edit. Phase 2 starts
   by proving that the files an analyzer reads under `-overlay` are the
   overlaid ones; until then `check-content` defers these rules.

## 3. The first analyzer: `go/forbidden-type-in-position`

```yaml
- id: go-weak-types-any
  type: go/forbidden-type-in-position
  severity: error
  scope:
    include: ['**/*.go']
  params:
    types: [any]          # "any" means the empty interface, by any spelling
    positions: [field, param, result, var, elem, map, type-arg]
  cure: ...
```

It resolves every type expression in the listed positions with `go/types` and
reports one whose type is identical (`types.Identical`) to a listed type,
whether it is written directly, through an alias from any package, or
inferred. Positions:

| position | covers |
|---|---|
| `field` | struct fields, embedded or named |
| `param` | function and method parameters, variadic included |
| `result` | function and method results |
| `var` | package and local `var` declarations and `:=` definitions, inferred types included |
| `elem` | slice, array, pointer, channel element types, nested at any depth |
| `map` | map key and value types, at any depth |
| `type-arg` | explicit and inferred type arguments at an instantiation |

A type parameter's constraint is never a position: `[T any]` is the
unconstrained generic the consumer's cure asks for. A position inside
generated code (`// Code generated ... DO NOT EDIT.`) is skipped, as the rest
of the engine skips it.

**Superset proof.** The consumer retires `go-weak-types-any` only when every
finding of the regex is also a finding of this rule over the same tree. That
check is the consumer's, run once at retirement. The fixtures here pin each
row of the table above, plus the alias, inferred and wrapped shapes the regex
misses.

## 4. Engine shape

- `internal/rules/gotypes`: the analyzer, the plan type, and the
  `rules.Checker` for each `go/...` type in the family. `CheckFile` only
  records the in-scope Go files; `FinalizeErr` runs `go vet` over their
  packages and turns the diagnostics into matches.
- `cmd/formwork`: the `FORMWORK_VETTOOL` dispatch, the first statement in
  `main`.
- `go vet` runs with `-json`, the module directory as its working directory,
  and the packages as import paths. The environment is the caller's, minus
  `GOFLAGS` entries that would change what is checked (`-tags` stays).
- Cost: the checkers are `CostHeavy` and process-bound, as `command` is, so
  they run in the width-capped pool and are timed like any other rule.

## 5. Phases

1. Infrastructure plus `go/forbidden-type-in-position`, whole-tree only. The
   checker is a finalizer, so `check-content` defers it with reason `finalizer`
   and lists it under `not_evaluated`, as it does every finalizer.
2. `check-content` through `-overlay`, which makes the rule eligible there.
3. One `go vet` for every `go/types` rule in a run, if the per-rule cost
   measured in phase 1 warrants the shared state.
4. Further analyzers from the consumer's list (`confine`, `layers`,
   `ctxlaunder`, `exhaustive`, `errcheckx`), each with fixtures and a mutation
   spec.

## 6. What this does not do

- It does not replace `go vet`'s own analyzers or staticcheck. It runs only
  the analyzers formwork declares.
- It does not load packages outside `go vet`. A tree with no `go.mod` above an
  in-scope file is an engine error for the rule, not a silent skip.
- A program other than `cmd/formwork` that evaluates these rules (a downstream
  analyser built on the `corpus` package) runs `go vet` with itself as the
  vettool, so its `main` must hand control to `gotypes.Main` under
  `FORMWORK_VETTOOL` as `cmd/formwork` does. Without that, `go vet` fails and
  the rule is an engine error, not a pass.
