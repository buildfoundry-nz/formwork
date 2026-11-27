# GCS lifecycle prefixes

Each staging prefix below is pinned to its sample-infra Age-1-day Delete
rule. Keep the Go constant, this entry, and the infra rule in lockstep.

- `classify-chunks/` — Age 1 day Delete — sample-infra modules/bucket-lifecycle prefix_expiry_rules via infra/<env>/main.tf drawings_bucket
