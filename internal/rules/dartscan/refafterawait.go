package dartscan

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"gopkg.in/yaml.v3"
)

const (
	defaultAccess = `ref\.(read|watch)\b`
	defaultGuard  = `mounted`
)

type refAfterAwaitParams struct {
	Method string `yaml:"method"`
	Access string `yaml:"access"`
	Guard  string `yaml:"guard"`
}

// refAfterAwait flags, within a method whose header matches Method, any line
// matching Access that occurs after a line containing `await` and is not
// guarded — heuristically, the access line itself or the immediately preceding
// `if` line contains a Guard token.
type refAfterAwait struct {
	method *regexp.Regexp
	access *regexp.Regexp
	guard  *regexp.Regexp
}

func newRefAfterAwait(node *yaml.Node) (rules.Checker, error) {
	var p refAfterAwaitParams
	if err := rules.DecodeParams(node, &p); err != nil {
		return nil, err
	}
	if p.Method == "" {
		return nil, errors.New("dart/ref-after-await: params.method is required")
	}
	method, err := regexp.Compile(p.Method)
	if err != nil {
		return nil, fmt.Errorf("dart/ref-after-await: invalid method regex: %w", err)
	}
	if p.Access == "" {
		p.Access = defaultAccess
	}
	access, err := regexp.Compile(p.Access)
	if err != nil {
		return nil, fmt.Errorf("dart/ref-after-await: invalid access regex: %w", err)
	}
	if p.Guard == "" {
		p.Guard = defaultGuard
	}
	guard, err := regexp.Compile(p.Guard)
	if err != nil {
		return nil, fmt.Errorf("dart/ref-after-await: invalid guard regex: %w", err)
	}
	return &refAfterAwait{method: method, access: access, guard: guard}, nil
}

func (c *refAfterAwait) guarded(line, prev string) bool {
	if c.guard.MatchString(line) {
		return true
	}
	// An immediately preceding if-guard line (e.g. `if (!mounted) return;`).
	return strings.Contains(prev, "if") && c.guard.MatchString(prev)
}

func (c *refAfterAwait) CheckFile(f *scan.File) ([]rules.Match, error) {
	if !isDart(f) {
		return nil, nil
	}
	lines, err := f.Lines()
	if err != nil {
		return nil, err
	}

	var matches []rules.Match
	inBody := false
	bodyStarted := false
	sawAwait := false
	depth := 0
	headerLine := 0

	for i, line := range lines {
		if !inBody {
			if !c.method.MatchString(line) {
				continue
			}
			if isAbstractOrArrow(line) {
				continue
			}
			inBody = true
			bodyStarted = false
			sawAwait = false
			depth = 0
			headerLine = i + 1
			// Fall through to process the header line's own content.
		}

		if sawAwait && c.access.MatchString(line) {
			var prev string
			if i > 0 {
				prev = lines[i-1]
			}
			if !c.guarded(line, prev) {
				matches = append(matches, rules.Match{
					Line:    i + 1,
					Message: fmt.Sprintf("access %q reached after await without a %q guard", c.access.String(), c.guard.String()),
				})
			}
		}
		if strings.Contains(line, "await") {
			sawAwait = true
		}

		depth += braceDelta(line)
		if strings.Contains(line, "{") {
			bodyStarted = true
		}
		if bodyStarted && depth <= 0 {
			inBody = false
		}
	}

	if inBody && bodyStarted {
		return nil, fmt.Errorf("%s: unterminated method body starting at line %d (braces never balanced)", f.Path(), headerLine)
	}
	return matches, nil
}

func init() {
	rules.Register("dart/ref-after-await", newRefAfterAwait)
}
