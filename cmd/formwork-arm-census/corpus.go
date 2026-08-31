package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

// arm is one rule arm as DECLARED, read straight off the YAML rather than off
// the compiled rule: config.Rule keeps the compiled checker and throws the
// params away, and both questions this census asks are about what the author
// wrote (a pattern that requires nothing; a floor of one witness).
type arm struct {
	File string // repo-relative rule file
	Line int    // 1-based line the arm's mapping opens on
	ID   string
	Type string

	Pattern string
	Mode    string
	Op      string
	N       int
	Syntax  string
}

// armSpec is the decode target. Unknown params keys are ignored, so every rule
// type in the corpus decodes — a `type: command` arm simply yields empty
// pattern/mode/op, which is exactly what "not a pattern arm" should look like.
type armSpec struct {
	ID     string `yaml:"id"`
	Type   string `yaml:"type"`
	Params struct {
		Pattern string `yaml:"pattern"`
		Mode    string `yaml:"mode"`
		Op      string `yaml:"op"`
		N       *int   `yaml:"n"`
		Syntax  string `yaml:"syntax"`
	} `yaml:"params"`
}

// loadCorpus reads every arm in root/.formwork/rules/*.yaml.
//
// Everything — params AND line numbers — comes from ONE yaml.Node decode. A
// pattern is an arbitrary quoted string that routinely contains `#`, `:` and
// backslash escapes, so reading params off a line regex is a false verdict in
// either direction; and reading the LINE off a `^\s*- id:` text scan assumes
// `id` is the arm's first key, which is a property of how the corpus happens to
// be written rather than of YAML. It is not even stable inside this repo:
// mutation-proof materialises each rule into a scratch corpus by re-marshalling
// the file (scripts/dev/mutation-proof/prove.go), and yaml.Marshal of a map
// emits keys ALPHABETICALLY — `cure:` opens the arm and `id:` lands three lines
// down. A text scan finds zero arm headers there and the census dies on a
// corpus the engine reads perfectly well. The node's own Line is the same fact
// without the assumption.
func loadCorpus(root string) ([]arm, error) {
	files, err := filepath.Glob(filepath.Join(root, ".formwork", "rules", "*.yaml"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	var out []arm
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		rel := filepath.ToSlash(relTo(root, f))
		arms, err := armsIn(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", rel, err)
		}
		for i := range arms {
			arms[i].File = rel
		}
		out = append(out, arms...)
	}
	return out, nil
}

// armsIn decodes one rule file into its arms, in declaration order.
//
// The document is walked as a node tree so each arm keeps the line its mapping
// opens on, and each arm's params are then decoded through the same struct the
// engine's own loader shape mirrors. A file whose top level is not a mapping
// with a `rules` sequence is an ERROR rather than an empty result: a rule file
// the census silently read as "no arms" is a rule file the census is not
// covering, which is the defect it exists to find.
func armsIn(data []byte) ([]arm, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil, fmt.Errorf("no YAML document")
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("top level is not a mapping")
	}
	var rules *yaml.Node
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "rules" {
			rules = root.Content[i+1]
			break
		}
	}
	if rules == nil {
		return nil, fmt.Errorf("no rules key")
	}
	if rules.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("rules is not a sequence")
	}
	out := make([]arm, 0, len(rules.Content))
	for _, n := range rules.Content {
		var s armSpec
		if err := n.Decode(&s); err != nil {
			return nil, fmt.Errorf("line %d: %w", n.Line, err)
		}
		a := arm{
			Line: n.Line, ID: s.ID, Type: s.Type,
			Pattern: s.Params.Pattern, Mode: s.Params.Mode, Op: s.Params.Op,
			Syntax: s.Params.Syntax,
		}
		if s.Params.N != nil {
			a.N = *s.Params.N
		}
		out = append(out, a)
	}
	return out, nil
}

func relTo(root, p string) string {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return p
	}
	return rel
}
