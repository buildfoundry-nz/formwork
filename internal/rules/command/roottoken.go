// roottoken.go — the engine owns the tree a detector reads (#28).
//
// A command rule used to name its tree with a path relative to the caller's
// working directory. `--root ../../..` is correct under `check`, where the
// cwd is the repository; under `test` the fixture runner makes the fixture
// tree the cwd and the same three segments land on the repository again, so
// the fixture judged the real tree. Nothing in the rule distinguished the two
// planes, because the rule was not the party that knew which plane it was in.
//
// The engine is. It walked or created every tree it evaluates, so it
// substitutes them:
//
//	{{root}} — the tree UNDER EVALUATION: the repository under check, the
//	           fixture tree under test, a scratch under a mutation run.
//	{{repo}} — the corpus's own tree, so a detector that lives in the
//	           repository is reachable while judging a fixture. The canonical
//	           shape is `go -C {{repo}}/scripts/dev/x run . --root {{root}}`.
//
// Under check the two are the same directory, which is why {{repo}} falls
// back to Root when the caller set no Repo: a caller that predates the field
// substitutes the tree it is evaluating rather than an empty path, and an
// empty path would have made the detector read the filesystem root.
package command

import (
	"fmt"
	"path/filepath"
	"strings"
)

const (
	rootToken = "{{root}}"
	repoToken = "{{repo}}"
)

// substituteRoots resolves the two tokens in every argument. repo empty means
// "the same tree as root" (the check plane).
//
// Both are made ABSOLUTE first. A CLI is routinely given a relative root —
// `formwork test -C .` makes it the literal "." — and a relative path in the
// argv is resolved by the CHILD against ITS working directory, which is the
// tree under evaluation. {{repo}} would then name the fixture rather than the
// corpus: measured as `go: cannot find main module, but found .git/config in
// <the fixture>`, a detector hunting its own module inside the tree it was
// asked to judge. An unresolvable path is left as written rather than
// guessed at; the tool's own error then names it.
func substituteRoots(argv []string, root, repo string) []string {
	if repo == "" {
		repo = root
	}
	root, repo = absOrAsWritten(root), absOrAsWritten(repo)
	out := make([]string, len(argv))
	for i, a := range argv {
		// Substitution only. The arguments are not all paths — a pattern, a
		// message, a shell body — and normalising them would rewrite values
		// the rule meant literally. Both tokens carry absolute, already-clean
		// paths, so there is nothing to normalise anyway.
		a = strings.ReplaceAll(a, rootToken, root)
		out[i] = strings.ReplaceAll(a, repoToken, repo)
	}
	return out
}

// absOrAsWritten makes p absolute, or returns it unchanged when the working
// directory cannot be read — the tool's own error is a better report than a
// path this function invented.
func absOrAsWritten(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}

// Argv is the rule's command line, for the surfaces that must judge it
// without running it — `formwork lint`'s parent-segment check. A copy, so a
// caller cannot reach in and rewrite what the rule will execute.
func (c *command) Argv() []string { return append([]string(nil), c.cmd...) }

// ParentSegmentArg reports the first argument naming a parent directory.
//
// It asks about PATH SEGMENTS, not about the characters `..`: an argument is
// refused when a `..` stands alone between separators (or at either end),
// which is what a filesystem reads as "up one". A regex like `a..b`, a
// version like `x...y`, an ellipsis in a message: none of those is a segment,
// none is refused. Getting that boundary wrong in the refusing direction
// would be the worse failure — a corpus that cannot express a pattern is a
// corpus that reaches for a shell script instead.
func ParentSegmentArg(argv []string) (string, bool) {
	for _, a := range argv {
		for _, field := range strings.Fields(a) {
			for _, seg := range strings.Split(filepath.ToSlash(field), "/") {
				if seg == ".." {
					return a, true
				}
			}
		}
	}
	return "", false
}

// ParentSegmentProblem is the lint finding, and it carries the migration
// rather than the complaint: the author is one substitution away from the
// shape that works on every plane.
//
// It is a LINT check rather than a load refusal (#28). Refusing at load makes
// every corpus written before the tokens unreadable, and the tools that read
// one are the ones that most need to: the vacuity census loads the corpus as
// it stood at a change's merge base to tell a new rule from an edited one,
// and a base it cannot parse is a transition it cannot compute. Lint runs on
// every pull request, so the shape still cannot merge; what it no longer does
// is make history unreadable.
func ParentSegmentProblem(ruleID, arg string) string {
	return fmt.Sprintf("%s: argv %q names a parent directory, so what this detector reads is decided by the caller's working directory rather than by the engine — under `formwork test` that is the fixture tree and `..` climbs out of it into the repository (#28). Name the tree instead: {{root}} is the tree under evaluation and {{repo}} the corpus's own tree, e.g. `go -C {{repo}}/scripts/dev/x run . --root {{root}}`", ruleID, arg)
}
