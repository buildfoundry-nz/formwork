// lintpolicy.go — the corpus-selectable check set (#89). Split from lint.go,
// which the 750-line vendor cap bounds; same package.
package meta

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// lintPolicyFile is the tracked declaration at .formwork/lint.yaml.
const lintPolicyPath = ".formwork/lint.yaml"

// nonSkippable names the checks a corpus may not switch off, with the reason
// each one is exempt from the mechanism, printed in the refusal.
//
// rules-present is here because it is the guard against the exact defect this
// whole file could otherwise buy back. It reports a config with no rules, under
// which every other check iterates nothing and reports OK — "a check that cannot
// fail is a check that passes" (#151). A skip list that can turn the vacuity
// guard off is a vacuity guard the vacuous config gets to decline, so the entry
// is refused at load, before any verdict or disclosure is printed.
//
// This is the same posture refuseUnreadableInScope takes by not consulting the
// policy at all: neither is a verdict about the corpus, both are preconditions
// for having one. They are spelled differently because that refusal is not a
// named check — there is no entry a corpus could write for it — while this one
// is, so the entry has to be answered rather than ignored.
var nonSkippable = map[string]string{
	"rules-present": "it reports a config that enforces nothing, under which every other check has nothing to examine and reports OK — a corpus that could skip it would be exempting itself from the one check that says its board is empty",
}

// lintPolicy is one corpus's declared, justified list of lint checks that do not
// apply to it.
//
// It exists because `formwork lint` had never run over examples/ and could not:
// the port corpora are thin slices of a much larger tree, so most of their rules
// legitimately match no file here and most of their excludes legitimately name
// generated directories this slice omits. Running the whole check set there
// reports hundreds of "problems" that are properties of the fixture material,
// and gating CI on a red board only teaches people to ignore it (#89).
//
// The declaration lives in the corpus, in tracked YAML, for the reason every
// escape hatch in this repo does: a skip list baked into a Makefile flag is an
// exemption nobody reviews. Here it sits beside the rules it exempts, each entry
// carrying the reason it exists, and every skip is printed on every run.
//
// It is DECLARED SEPARATELY from formwork.yaml, and that is a boundary rather
// than an oversight: which checks lint runs is meaningful to lint alone, so
// putting it in the config every command loads would make `check` and `test`
// decode a key they can never honour.
type lintPolicy struct {
	entries []lintSkipEntry
	byCheck map[string]string
	seen    map[string]bool
	order   []string // skipped checks, in the order the run reached them
}

type lintSkipEntry struct {
	Check  string `yaml:"check"`
	Reason string `yaml:"reason"`
}

type lintPolicyDoc struct {
	Version int             `yaml:"version"`
	Skip    []lintSkipEntry `yaml:"skip"`
}

// loadLintPolicy reads root's .formwork/lint.yaml. An absent file is the empty
// policy — every check runs, and the output is byte-for-byte what it was before
// this mechanism existed. Every other failure to make sense of the file is an
// engine/config error (exit 2), never a quietly empty policy: a skip list that
// fails open is a check set nobody declared.
func loadLintPolicy(root string) (*lintPolicy, error) {
	p := &lintPolicy{byCheck: map[string]string{}, seen: map[string]bool{}}
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(lintPolicyPath)))
	if errors.Is(err, os.ErrNotExist) {
		return p, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lint: reading %s: %w", lintPolicyPath, err)
	}

	var doc lintPolicyDoc
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true) // strict decoding, spec §4: an unknown field is exit 2
	if err := dec.Decode(&doc); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("lint: %s: %w", lintPolicyPath, err)
	}
	if doc.Version != 1 {
		return nil, fmt.Errorf("lint: %s: unsupported version %d (this binary supports version 1)", lintPolicyPath, doc.Version)
	}
	for i, e := range doc.Skip {
		if strings.TrimSpace(e.Check) == "" {
			return nil, fmt.Errorf("lint: %s: skip[%d]: check must name the lint check being skipped", lintPolicyPath, i)
		}
		if strings.TrimSpace(e.Reason) == "" {
			return nil, fmt.Errorf("lint: %s: skip[%d]: skipping %q needs a reason — a check nobody runs is an exemption, and this repo's exemptions carry their justification", lintPolicyPath, i, e.Check)
		}
		if why, no := nonSkippable[strings.TrimSpace(e.Check)]; no {
			return nil, fmt.Errorf("lint: %s: skip[%d]: %q cannot be skipped — %s", lintPolicyPath, i, e.Check, why)
		}
		if _, dup := p.byCheck[e.Check]; dup {
			return nil, fmt.Errorf("lint: %s: duplicate skip entry for %q — one check, one reason", lintPolicyPath, e.Check)
		}
		p.byCheck[e.Check] = strings.TrimSpace(e.Reason)
		p.entries = append(p.entries, e)
	}
	return p, nil
}

// skipping reports whether check is declared skipped, disclosing it on w the
// first time it is asked. Callers gate the check's WORK on this, not only its
// output line: a skipped check that still computes can still fail the run with
// an error, which would make the skip a lie about what lint did.
func (p *lintPolicy) skipping(w io.Writer, check string) bool {
	reason, ok := p.byCheck[check]
	if !ok {
		return false
	}
	if !p.seen[check] {
		p.seen[check] = true
		p.order = append(p.order, check)
	}
	fmt.Fprintf(w, "[%s] SKIPPED — %s\n", check, reason)
	return true
}

// unusedErr reports any declared skip a WHOLE-CORPUS run never reached.
//
// Fail-closed, and it is the reason this mechanism needs no hand-maintained
// registry of check names: the authority on what lint runs is the run itself.
// A typo, a check that was renamed, and a check this corpus never reaches are
// all the same defect — an entry protecting nothing, which is the dead-exclude
// rot (#55) one level up, in the one file whose whole purpose is to be read as
// "these checks were deliberately not run".
//
// scoped disarms it, because under `--rule` the run itself is no longer that
// authority. `--rule` narrows cfg.Rules to one, so every conditional check
// (prefilter-load-bearing, command-trigger-armable) is unarmed by the NARROWING
// for almost any rule id, and this refusal cannot tell that apart from a corpus
// that never arms it. Left armed, a valid config plus a valid rule id exited 2 —
// the exit-code contract inverted, since 2 means the engine or the config is
// broken and neither is. Dead config rots in the file, not in one inner-loop
// invocation, so the whole-corpus run that `make lint` performs is where the
// refusal has to hold, and it still does.
func (p *lintPolicy) unusedErr(scoped bool) error {
	if scoped {
		return nil
	}
	var dead []string
	for _, e := range p.entries {
		if !p.seen[e.Check] {
			dead = append(dead, e.Check)
		}
	}
	if len(dead) == 0 {
		return nil
	}
	return fmt.Errorf("lint: %s declares a skip for %s, which this run never reached — either the name is not a check this binary runs, or the check is conditional and this corpus never armed it; a skip that suppresses nothing is dead config, so remove the entry",
		lintPolicyPath, strings.Join(quoteAll(dead), ", "))
}

// summarySuffix renders the skipped checks for the summary line, empty when
// nothing was skipped so an unpolicied repo's summary is unchanged.
func (p *lintPolicy) summarySuffix() string {
	if len(p.order) == 0 {
		return ""
	}
	return fmt.Sprintf(" (%d skipped: %s — see %s)", len(p.order), strings.Join(p.order, ", "), lintPolicyPath)
}

func quoteAll(ss []string) []string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		out = append(out, fmt.Sprintf("%q", s))
	}
	return out
}
