package vacuity

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/buildfoundry-nz/formwork/internal/scan"
	"gopkg.in/yaml.v3"
)

// exceptMarker is the comment that declares an except.paths entry inert: the
// path it names is a file the rule's pattern could never fire on, listed so the
// roster stays complete and auditable. It must sit on the line directly above
// the entry and carry a reason; a bare marker declares nothing.
const exceptMarker = "# except-declaration:"

// ExceptEntry is one except.paths glob of one rule as written in a rule file.
type ExceptEntry struct {
	Glob     string
	Line     int    // 1-based line of the glob scalar
	Declared bool   // `# except-declaration: <non-empty reason>` on the line above
	Reason   string // the text after the colon when Declared
}

// RuleExcepts is one rule's id and its except.paths entries.
type RuleExcepts struct {
	ID      string
	Entries []ExceptEntry
}

// ExceptEntries reads the except.paths globs of every rule in one rule file's
// source text, one RuleExcepts per rule, in the order of the file's `rules:`
// sequence. The marker is a YAML comment, so it exists only in the
// source text — config.Rule cannot carry it. This is the one parser of it: the
// census and `formwork test` both read the marker here.
func ExceptEntries(data []byte) ([]RuleExcepts, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	var out []RuleExcepts
	for _, rule := range seqUnder(&doc, "rules") {
		var entries []ExceptEntry
		for _, g := range seqUnder(mapValue(rule, "except"), "paths") {
			g = resolveAlias(g)
			if g == nil || g.Kind != yaml.ScalarNode {
				continue
			}
			e := ExceptEntry{Glob: g.Value, Line: g.Line}
			if g.Line >= 2 {
				if rest, ok := strings.CutPrefix(strings.TrimSpace(lines[g.Line-2]), exceptMarker); ok {
					e.Reason = strings.TrimSpace(rest)
					e.Declared = e.Reason != ""
				}
			}
			entries = append(entries, e)
		}
		id := ""
		if v := mapValue(rule, "id"); v != nil {
			id = v.Value
		}
		out = append(out, RuleExcepts{ID: id, Entries: entries})
	}
	return out, nil
}

// DeclaredInert maps a rule id to the except.paths globs its author declared
// inert (see ExceptEntry.Declared).
type DeclaredInert map[string][]string

// ReadDeclaredInert reads root/.formwork/rules/*.yaml. A file that cannot be
// read or parsed is an error: an unreadable declaration must not quietly turn
// into "nothing is declared".
func ReadDeclaredInert(root string) (DeclaredInert, error) {
	files, err := filepath.Glob(filepath.Join(root, ".formwork", "rules", "*.yaml"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	out := DeclaredInert{}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("reading declared-inert paths: %w", err)
		}
		perRule, err := ExceptEntries(data)
		if err != nil {
			return nil, fmt.Errorf("reading declared-inert paths: %s: %w", f, err)
		}
		for _, re := range perRule {
			for _, e := range re.Entries {
				if e.Declared {
					out[re.ID] = append(out[re.ID], e.Glob)
				}
			}
		}
	}
	return out, nil
}

// Excuses reports whether any file of fs matches an except.paths glob rule
// ruleID declared inert. A nil DeclaredInert excuses nothing.
func (d DeclaredInert) Excuses(ruleID string, fs *scan.FileSet) bool {
	for _, glob := range d[ruleID] {
		for _, f := range fs.Files {
			if ok, _ := doublestar.Match(glob, f.Path()); ok {
				return true
			}
		}
	}
	return false
}

func resolveAlias(n *yaml.Node) *yaml.Node {
	for n != nil && n.Kind == yaml.AliasNode {
		n = n.Alias
	}
	return n
}

func mapValue(n *yaml.Node, key string) *yaml.Node {
	n = resolveAlias(n)
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return resolveAlias(n.Content[i+1])
		}
	}
	return nil
}

// seqUnder returns the items of the sequence under key in n, or in n's root
// mapping when n is a document node.
func seqUnder(n *yaml.Node, key string) []*yaml.Node {
	n = resolveAlias(n)
	if n == nil {
		return nil
	}
	if n.Kind == yaml.DocumentNode {
		if len(n.Content) == 0 {
			return nil
		}
		n = resolveAlias(n.Content[0])
	}
	seq := mapValue(n, key)
	if seq == nil || seq.Kind != yaml.SequenceNode {
		return nil
	}
	return seq.Content
}
