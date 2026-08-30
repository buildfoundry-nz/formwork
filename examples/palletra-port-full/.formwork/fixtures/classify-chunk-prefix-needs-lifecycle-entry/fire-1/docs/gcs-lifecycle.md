# GCS lifecycle prefixes

The Go prefix has drifted from its infra pairing: the manifest entry lost the
`prefix_expiry_rules` reference, so the cross-repo Delete contract no longer
resolves.

- `classify-chunks/` — Age 1 day Delete — palletra-infra modules/bucket-lifecycle
