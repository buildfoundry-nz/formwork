package pattern

import (
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"gopkg.in/yaml.v3"
)

type requiredParams struct {
	Pattern string `yaml:"pattern"`
	Mode    string `yaml:"mode"`
	Syntax  string `yaml:"syntax"` // "" | re2 | regexp2
	// When narrows every-file mode to the files that match it: "a file that
	// does X must contain Y". It is a requirement, not an excusal, and is the
	// form a file-wide proof takes now that forbidden-pattern evidence must sit
	// beside its trigger.
	When string `yaml:"when"`
	// Multiline matches pattern (and when) over the whole file, so Y may span
	// lines (a method body shape, an ordered pair of statements).
	Multiline bool `yaml:"multiline"`
}

const (
	modeEveryFile = "every-file"
	modeExists    = "exists"
)

type required struct {
	re        lineMatcher
	mode      string
	when      lineMatcher // nil: every in-scope file
	multiline bool

	seen  atomic.Bool // exists mode: any in-scope file observed
	found atomic.Bool // exists mode: any in-scope file matched
}

func newRequired(params *yaml.Node) (rules.Checker, error) {
	var p requiredParams
	if err := rules.DecodeParams(params, &p); err != nil {
		return nil, err
	}
	if p.Pattern == "" {
		return nil, errors.New("required-pattern: params.pattern is required")
	}
	re, err := compileMatcher("required-pattern", p.Pattern, p.Syntax)
	if err != nil {
		return nil, err
	}
	mode := p.Mode
	if mode == "" {
		mode = modeEveryFile
	}
	if mode != modeEveryFile && mode != modeExists {
		return nil, fmt.Errorf("required-pattern: unknown mode %q (want %q or %q)", p.Mode, modeEveryFile, modeExists)
	}
	c := &required{re: re, mode: mode, multiline: p.Multiline}
	if p.When != "" {
		if mode != modeEveryFile {
			return nil, errors.New("required-pattern: when applies to every-file mode")
		}
		if c.when, err = compileMatcher("required-pattern when", p.When, p.Syntax); err != nil {
			return nil, err
		}
	}
	return c, nil
}

// matches reports whether m matches f: over the whole content when multiline,
// else on some line.
func (c *required) matches(f *scan.File, m lineMatcher) (bool, error) {
	if c.multiline {
		content, err := f.Content()
		if err != nil {
			return false, err
		}
		return m.MatchString(string(content))
	}
	lines, err := f.Lines()
	if err != nil {
		return false, err
	}
	for _, line := range lines {
		ok, err := m.MatchString(line)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

func (c *required) CheckFile(f *scan.File) ([]rules.Match, error) {
	if c.when != nil {
		applies, err := c.matches(f, c.when)
		if err != nil {
			return nil, err
		}
		if !applies {
			return nil, nil
		}
	}
	matched, err := c.matches(f, c.re)
	if err != nil {
		return nil, err
	}
	if c.mode == modeExists {
		c.seen.Store(true)
		if matched {
			c.found.Store(true)
		}
		return nil, nil
	}
	if !matched {
		return []rules.Match{{Line: 0, Message: "required pattern missing: " + c.re.String()}}, nil
	}
	return nil, nil
}

// Finalize reports the exists-mode verdict once all files have been checked.
// A scope that matched zero files passes here — this checker judges content and
// has none to judge, so a verdict either way would be invented. Empty-scope rot
// is reported one level up and by identity rather than by rule type: `check`'s
// scan summary discloses such rules on a whole-tree run, and `formwork lint`'s
// empty-scope fails them (spec §11). This is why the checker is not the altitude for it —
// a rule that never fired is a fact about the SCOPE, and the scope is the same
// question whatever type sits behind it.
func (c *required) Finalize() []rules.Match {
	if c.mode != modeExists {
		return nil
	}
	if c.seen.Load() && !c.found.Load() {
		return []rules.Match{{Message: "required pattern not found in any in-scope file: " + c.re.String()}}
	}
	return nil
}

// WholeTreeInvariant reports true only in exists mode: the verdict ("some
// in-scope file matched") is non-monotonic under file removal, so a changeset
// scan must evaluate it over the whole tree (#4). every-file mode is
// per-file/monotonic and stays range-scopeable.
func (c *required) WholeTreeInvariant() bool {
	return c.mode == modeExists
}

func init() {
	rules.Register("required-pattern", newRequired)
}
