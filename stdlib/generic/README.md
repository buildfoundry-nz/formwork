# generic — portable hygiene pack

Rules that apply to any Go / Dart / SQL repo. They are the ones the TakeoffQS
gate survey classified as **generic**: weak types, no `skip:` in tests, no
TODO/FIXME/HACK markers, immutable migrations, no new JS/TS, and the rest of
the portable set.

This directory is a formwork corpus. `formwork test -C stdlib/generic` and
`formwork lint -C stdlib/generic` prove it.

Adopters do **not** copy these files. A pinned formwork binary embeds the rule
YAML. In the adopting repo:

```yaml
# .formwork/formwork.yaml
version: 1
engine: ">= 0.6.0"
library: [generic]
```

A local `.formwork/rules/` file that restates the same `id` replaces the pack
rule (rebind globs or attach an allowlist).

Not in this pack: `type: command` detectors that `go run` a product-local
script, and CI-wiring pins that name a specific workflow job. Those stay in
the adopting repo.

Product-domain rules (the Palletra / TakeoffQS dump) are `examples/palletra-port-*`,
not this pack.
