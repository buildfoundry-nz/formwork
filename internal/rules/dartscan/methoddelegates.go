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

type methodDelegatesParams struct {
	Method   string `yaml:"method"`
	MustCall string `yaml:"must_call"`
}

// methodDelegates flags a method whose header matches Method but whose body
// (tracked by brace depth) contains no line matching MustCall. The finding is
// anchored to the method header line.
type methodDelegates struct {
	method   *regexp.Regexp
	mustCall *regexp.Regexp

	// anchor tracks whether `method` selected a checkable body anywhere in
	// scope. Without it the rule is fail-OPEN: renaming the method makes the
	// header match nothing, no body is ever entered, and the delegation
	// invariant is retired with no finding.
	anchor rules.AnchorProbe
}

func newMethodDelegates(node *yaml.Node) (rules.Checker, error) {
	var p methodDelegatesParams
	if err := rules.DecodeParams(node, &p); err != nil {
		return nil, err
	}
	if p.Method == "" {
		return nil, errors.New("dart/method-delegates: params.method is required")
	}
	method, err := regexp.Compile(p.Method)
	if err != nil {
		return nil, fmt.Errorf("dart/method-delegates: invalid method regex: %w", err)
	}
	if p.MustCall == "" {
		return nil, errors.New("dart/method-delegates: params.must_call is required")
	}
	mustCall, err := regexp.Compile(p.MustCall)
	if err != nil {
		return nil, fmt.Errorf("dart/method-delegates: invalid must_call regex: %w", err)
	}
	return &methodDelegates{method: method, mustCall: mustCall}, nil
}

func (c *methodDelegates) CheckFile(f *scan.File) ([]rules.Match, error) {
	if !isDart(f) {
		return nil, nil
	}
	lines, err := f.Lines()
	if err != nil {
		return nil, err
	}
	c.anchor.Observe()

	var matches []rules.Match
	inBody := false
	bodyStarted := false
	found := false
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
			// Only a body the rule can actually walk counts as the anchor
			// being alive — an abstract or arrow declaration was skipped
			// above, so an interface stub must not keep a renamed
			// implementation's invariant looking held.
			c.anchor.Hit()
			inBody = true
			bodyStarted = false
			found = false
			depth = 0
			headerLine = i + 1
			// Fall through to process the header line's own content.
		}

		if c.mustCall.MatchString(line) {
			found = true
		}

		depth += braceDelta(line)
		if strings.Contains(line, "{") {
			bodyStarted = true
		}
		if bodyStarted && depth <= 0 {
			inBody = false
			if !found {
				matches = append(matches, rules.Match{
					Line:    headerLine,
					Message: fmt.Sprintf("method body does not call required %q", c.mustCall.String()),
				})
			}
		}
	}

	if inBody && bodyStarted {
		return nil, fmt.Errorf("%s: unterminated method body starting at line %d (braces never balanced)", f.Path(), headerLine)
	}
	return matches, nil
}

// Finalize reports a method anchor that selected no checkable body anywhere in
// scope. Scope-wide, so a sibling Dart file that simply does not declare the
// method is compliant; only a scope in which nothing declares it is a finding.
func (c *methodDelegates) Finalize() []rules.Match {
	return c.anchor.Verdict("method anchor", c.method.String())
}

// WholeTreeInvariant is true: "no in-scope file declares the method" is
// non-monotonic under file removal, so a changeset scan must still evaluate it
// over the whole tree (spec §8).
func (c *methodDelegates) WholeTreeInvariant() bool { return true }

func init() {
	rules.Register("dart/method-delegates", newMethodDelegates)
}
