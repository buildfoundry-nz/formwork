// generated.go — the one definition of generated output (formwork.yaml
// `generated:`). Split from config.go, which the 750-line vendor cap bounds.
//
// WHY ONE DEFINITION. A rule about hand-written code does not judge what a
// generator wrote: the generator's output is correct when it reproduces from
// its input, and that is a different gate. Before 0.14 every such rule carried
// its own list of generator spellings (`**/*.g.dart`, `**/*.pb.go`,
// `shared/generated/**`, ...) — one copy per rule, each a path region a
// hand-written file could be parked in, and each silently wrong the day a
// generator was added or renamed its output.
//
// A generated file says what it is: every generator writes a header. The
// repository declares those headers ONCE, here, and each declaration names
// the rule that PROVES a file carrying it reproduces from its generator
// (`proven_by`). That proof is what keeps the marker from being a self-granted
// exemption: a hand-written file that copies the header fails the freshness
// gate, because regenerating does not produce it.
//
// A file is generated when a declared marker appears in its LEADING comment
// block — the lines before its first line that is neither blank nor a
// comment. A marker anywhere later (a string, a comment beside code) declares
// nothing. Generated files are outside every rule's evaluation unless the rule
// says `scope.generated: true`, and the escape-hatch census counts them on
// every run.
package config

import (
	"bytes"
	"fmt"
	"strings"
)

// GeneratedMarker is one declared generator header and the rules that prove a
// file carrying it reproduces from its generator.
type GeneratedMarker struct {
	Marker   string
	ProvenBy []string
}

// Generation is the repository's definition of generated output. A nil
// *Generation declares none.
type Generation struct {
	Markers []GeneratedMarker
}

type generatedSpec struct {
	Marker   string   `yaml:"marker"`
	ProvenBy []string `yaml:"proven_by"`
}

func compileGenerated(specs []generatedSpec) (*Generation, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	g := &Generation{}
	seen := map[string]bool{}
	for i, s := range specs {
		where := fmt.Sprintf("generated[%d]", i)
		if strings.TrimSpace(s.Marker) == "" {
			return nil, fmt.Errorf("%s: marker is empty; an empty marker matches every file's header", where)
		}
		if seen[s.Marker] {
			return nil, fmt.Errorf("%s: marker %q is declared twice", where, s.Marker)
		}
		seen[s.Marker] = true
		if len(s.ProvenBy) == 0 {
			return nil, fmt.Errorf("%s: marker %q: proven_by is required: the rule that proves a file carrying this header reproduces from its generator, without which the header is a self-granted exemption", where, s.Marker)
		}
		g.Markers = append(g.Markers, GeneratedMarker{Marker: s.Marker, ProvenBy: append([]string(nil), s.ProvenBy...)})
	}
	return g, nil
}

// Declares reports which declared marker the file's leading comment block
// carries, and whether one does.
func (g *Generation) Declares(content []byte) (string, bool) {
	if g == nil {
		return "", false
	}
	for len(content) > 0 {
		line := content
		if i := bytes.IndexByte(content, '\n'); i >= 0 {
			line, content = content[:i], content[i+1:]
		} else {
			content = nil
		}
		t := bytes.TrimSpace(line)
		if len(t) == 0 {
			continue
		}
		if !isCommentLine(t) {
			return "", false
		}
		for _, m := range g.Markers {
			if bytes.Contains(line, []byte(m.Marker)) {
				return m.Marker, true
			}
		}
	}
	return "", false
}

func isCommentLine(t []byte) bool {
	for _, p := range []string{"//", "#", "/*", "*", "--", "<!--"} {
		if bytes.HasPrefix(t, []byte(p)) {
			return true
		}
	}
	return false
}

// validateProvenBy refuses a marker whose proving rule is not in the corpus:
// a header nothing proves is a self-granted exemption.
func (g *Generation) validateProvenBy(rules []*Rule) error {
	if g == nil {
		return nil
	}
	ids := make(map[string]bool, len(rules))
	for _, r := range rules {
		ids[r.ID] = true
	}
	for _, m := range g.Markers {
		for _, id := range m.ProvenBy {
			if !ids[id] {
				return fmt.Errorf("generated: marker %q: proven_by %q names no rule in the corpus", m.Marker, id)
			}
		}
	}
	return nil
}

// Generation returns the repository's definition of generated output, shared
// by every rule loaded from it; nil for a rule built with New.
func (r *Rule) Generation() *Generation { return r.generation }

// JudgesGenerated reports whether the rule declares scope.generated: true, the
// one way a rule reads files a declared generator wrote.
func (r *Rule) JudgesGenerated() bool { return r.judgesGenerated }
