// scan.go — the formwork.yaml `scan:` block: the two repo-global channels
// that remove paths from the shared walk before any rule sees them, and their
// load-time validation. Split from config.go, which the 750-line vendor cap
// bounds; same package.
package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// GitignoreEntry is the scan.gitignore declaration: a standing instruction to
// prune every path git itself refuses to track.
//
// There is deliberately NO boolean here. A `prune: false` spelling would add a
// third state — declared but inert — that reads at a glance like the channel
// is active, and this is the widest exemption channel in the engine; the only
// way to turn it off is to delete the block, which no reader can misread.
//
// Reason is required by schema (exit 2 without it) for the same purpose it is
// required on IgnoreEntry: this hides paths from every FileSet-consuming rule
// at once, and a channel that wide must say in the config why it exists,
// rather than relying on a lint check that might not run.
type GitignoreEntry struct {
	Reason string
}

// IgnoreEntry is one scan.ignore glob plus its mandatory justification.
// scan.ignore hides matching paths from every FileSet-consuming rule in the
// run — the widest exemption channel in the engine — which is why Reason is
// required by schema (exit 2 without it) rather than by a lint check that
// might not run. External-tool rules (command, git-diff) shell out and rescan
// on their own, so they can still SEE ignored trees: extra findings, never
// silently missed ones (spec §5).
type IgnoreEntry struct {
	Glob   string
	Reason string
}

// IgnoreGlobs returns the glob half of every scan.ignore entry, in config
// order. This is what scan.WalkIgnoring consumes; lint keeps the full
// entries for the census.
func (c *Config) IgnoreGlobs() []string {
	out := make([]string, len(c.Ignore))
	for i, e := range c.Ignore {
		out[i] = e.Glob
	}
	return out
}

// scanSpec.Ignore decodes as a POINTER slice deliberately: yaml.v3 silently
// drops !!null items (`- null`, a bare `-`) when decoding a sequence into a
// value-struct slice, which would make a malformed entry vanish with exit 0 —
// a strict-decoding fail-open. Pointers preserve the null as a nil element so
// compileIgnore can reject it loudly.
//
// scanSpec.Gitignore is a POINTER for the same class of reason: `gitignore:`
// with an empty body must be rejected as a declaration missing its reason, not
// silently read as "the key is absent". A value struct cannot tell those apart.
type scanSpec struct {
	Ignore    []*ignoreSpec  `yaml:"ignore"`
	Gitignore *gitignoreSpec `yaml:"gitignore"`
}

type ignoreSpec struct {
	Glob   string `yaml:"glob"`
	Reason string `yaml:"reason"`
}

type gitignoreSpec struct {
	Reason string `yaml:"reason"`
}

// compileIgnore validates scan.ignore at load time so match time can use
// the house validate-at-load / ignore-error-at-match idiom (see matchAny).
func compileIgnore(specs []*ignoreSpec) ([]IgnoreEntry, error) {
	out := make([]IgnoreEntry, 0, len(specs))
	for i, s := range specs {
		if s == nil {
			return nil, fmt.Errorf("scan.ignore[%d]: null entry (each entry needs glob and reason)", i)
		}
		if s.Glob == "" {
			return nil, fmt.Errorf("scan.ignore[%d]: glob is required", i)
		}
		if strings.HasPrefix(s.Glob, "/") || strings.HasPrefix(s.Glob, "./") {
			return nil, fmt.Errorf("scan.ignore[%d]: glob %q must be repo-relative (no leading / or ./)", i, s.Glob)
		}
		if !doublestar.ValidatePattern(s.Glob) {
			return nil, fmt.Errorf("scan.ignore[%d]: invalid glob %q", i, s.Glob)
		}
		if strings.TrimSpace(s.Reason) == "" {
			return nil, fmt.Errorf("scan.ignore[%d] (%s): reason is required — scan.ignore hides paths from every rule", i, s.Glob)
		}
		out = append(out, IgnoreEntry{Glob: s.Glob, Reason: strings.TrimSpace(s.Reason)})
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// compileGitignore validates scan.gitignore at load time, same
// validate-at-load idiom as compileIgnore. A nil spec means the key is absent
// — or written with no body at all, which yaml.v3 decodes identically and
// which TestLoadScanGitignoreNullBodyIsOff pins as off on purpose. Either way,
// off prunes nothing, so the walk can only be wider than declared, never
// narrower.
func compileGitignore(s *gitignoreSpec) (*GitignoreEntry, error) {
	if s == nil {
		return nil, nil
	}
	if strings.TrimSpace(s.Reason) == "" {
		return nil, errors.New("scan.gitignore: reason is required — scan.gitignore hides every git-ignored path from every rule")
	}
	return &GitignoreEntry{Reason: strings.TrimSpace(s.Reason)}, nil
}
