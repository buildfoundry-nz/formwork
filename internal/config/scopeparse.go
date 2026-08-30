// scopeparse.go — the parsers for the rule `scope:` block. Split from
// config.go, which the 750-line vendor cap bounds; same package.
package config

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// parseMinFiles decodes the scope.min_files node. An absent key is 0 — no
// floor, the behaviour that predates the key. Anything that is not a YAML
// integer is an error, including a float, a quoted number, and an explicit
// `min_files:` with no value: all three are edits that did not finish, and the
// alternative to refusing them is arming a rule at a number nobody wrote.
func parseMinFiles(n yaml.Node) (int, error) {
	if n.Kind == 0 {
		return 0, nil
	}
	if n.Kind != yaml.ScalarNode || n.Tag != "!!int" {
		got := strings.TrimPrefix(n.Tag, "!!")
		if n.Value != "" {
			got += " " + n.Value
		}
		return 0, fmt.Errorf("want a non-negative integer, got %s", got)
	}
	var v int
	// The !!int tag above rules out the coercions. Decode's error is still
	// returned rather than ignored: swallowing it would arm the rule at 0, the
	// fail-open direction for a floor.
	if err := n.Decode(&v); err != nil {
		return 0, err
	}
	return v, nil
}

// parseExcludeEntries decodes a YAML sequence of scope.exclude globs and
// captures each entry's head/line comment as the justification string used by
// dead-exclude hygiene. A null/missing node yields nil,nil,nil. A non-sequence
// node is a config error.
func parseExcludeEntries(n yaml.Node) (globs []string, comments []string, err error) {
	if n.Kind == 0 {
		return nil, nil, nil
	}
	if n.Kind != yaml.SequenceNode {
		return nil, nil, fmt.Errorf("want a sequence, got %v", n.Kind)
	}
	for _, item := range n.Content {
		if item.Kind != yaml.ScalarNode {
			return nil, nil, fmt.Errorf("entry must be a string glob, got kind %v", item.Kind)
		}
		globs = append(globs, item.Value)
		// Prefer line comment ("- 'glob' # reason"), then head comment on the
		// entry (the block of '#' lines immediately above it).
		c := strings.TrimSpace(item.LineComment)
		if c == "" {
			c = strings.TrimSpace(item.HeadComment)
		}
		// yaml.v3 keeps the leading '#' on comments; strip every line's '#'.
		if c != "" {
			var parts []string
			for _, line := range strings.Split(c, "\n") {
				line = strings.TrimSpace(line)
				line = strings.TrimPrefix(line, "#")
				line = strings.TrimSpace(line)
				if line != "" {
					parts = append(parts, line)
				}
			}
			c = strings.Join(parts, " ")
		}
		comments = append(comments, c)
	}
	return globs, comments, nil
}
