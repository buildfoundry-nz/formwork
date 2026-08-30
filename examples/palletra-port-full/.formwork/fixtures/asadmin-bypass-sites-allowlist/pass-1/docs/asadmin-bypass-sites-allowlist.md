# AsSuperuser allowlist

## A. Org bootstrap

- `freightworks/services/core-api/routes/orgs/orgs.go` **[PERMANENT]** — org bootstrap must seed the first membership before any org-scoped RLS predicate can resolve, so it runs under the super-admin role flip.
