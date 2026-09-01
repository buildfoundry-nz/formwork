# .husky/ — git hooks

Markdown is documentation, not a hook. Prose here names the bash-4 constructs
the hooks must avoid — `declare -A`, `mapfile`, `gensub(` — and must not fire:
a `#` opens a heading here, not a shell comment, so the rule's `^[^#]*`
comment-immunity guard does not hold. Load-bearing for the `.husky/*.md`
exclusion.
