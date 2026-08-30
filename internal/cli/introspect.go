package cli

// The introspection surface (#105/#106/#108): explain, list, and rules-for
// render what the loaded config and the built-in registries already know —
// never a second source of truth. All three share the -format human|json
// flag; JSON output is deterministic (fixed-field structs, sorted slices).

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/preprocess"
	"github.com/buildfoundry-nz/formwork/internal/rules"
)

// loadGatedNonEmpty is loadGated for the introspection commands whose answer is
// DERIVED FROM THE CONFIG — list rules, list lanes, rules-for. A corpus that
// loaded no rules at all cannot be enumerated or queried, and rendering that as
// nothing (`[]` under -format json) is a wrong-frame answer, not a truthful
// empty one: the consumer asked what this repository enforces and was told,
// confidently, nothing. The three causes are the same ones rules-for's refusal
// has always named — a bad checkout, the wrong -C, a .formwork/rules that never
// materialised — and none of them is curable from an empty result.
//
// The condition is that the CONFIG LOADED NO RULES, not that this particular
// enumeration came out empty, and those are not the same case: `list lanes`
// over a corpus that declares lanes but no rules refuses as well, and its list
// would NOT have been empty — those lanes are simply not runnable. So each
// caller's consequence must state its own true reason; only the callers whose
// answer really would be empty (list rules, rules-for) may say so.
//
// consequence is the caller's half of noRulesReason's sentence, so the wording
// stays under one owner and the family cannot drift into three explanations of
// one condition (#157: the family being INCONSISTENT is what actually misled —
// a consumer that learned "formwork refuses a config it could not load" from
// rules-for carried that to list, which answered).
//
// Deliberately NOT reached by `list types` / `list preprocessors`: those come
// from the registries, never load config, and can never be empty — a guard
// covering them would refuse a question the config has no bearing on. That is
// why this is a per-kind call inside the switch rather than a gate in front of
// it, and why it is not folded into loadGated, whose other callers (check, test,
// lint, scope, hooks, explain) answer the zero-rule config their own ways.
func loadGatedNonEmpty(root, consequence string, stderr io.Writer) (*config.Config, bool) {
	cfg, ok := loadGated(root, stderr)
	if !ok {
		return nil, false
	}
	if len(cfg.Rules) == 0 {
		fmt.Fprintln(stderr, "formwork:", noRulesReason(cfg, consequence))
		return nil, false
	}
	return cfg, true
}

// introspectFormat validates the shared -format flag. Anything but the two
// known values is exit 2 naming the value — never a silent default.
func introspectFormat(f string, stderr io.Writer) bool {
	if f == "human" || f == "json" {
		return true
	}
	fmt.Fprintf(stderr, "formwork: unknown format %q (want human or json)\n", f)
	return false
}

func emitJSON(w, stderr io.Writer, v any) int {
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		// Unreachable with the fixed-field structs marshaled today, but an
		// exit 2 must never be mute (fail-open review finding 4).
		fmt.Fprintf(stderr, "formwork: rendering json: %v\n", err)
		return 2
	}
	fmt.Fprintln(w, string(out))
	return 0
}

// ruleRow is one rule in `list rules` JSON output.
type ruleRow struct {
	ID         string   `json:"id"`
	Type       string   `json:"type"`
	Severity   string   `json:"severity"`
	Cost       string   `json:"cost"`
	Preprocess string   `json:"preprocess,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	Lanes      []string `json:"lanes"`
}

// lanesOf resolves which lanes run r via the engine's own Lane.Selects —
// never a re-implementation of the selector logic, which could silently
// diverge (e.g. miss the cost filter). "Which lane runs rule X" is part of
// the list/explain enumeration promise (spec §10, #119 review finding 7).
func lanesOf(cfg *config.Config, r *config.Rule) []string {
	out := []string{}
	for _, l := range cfg.Lanes { // Config sorts lanes by name
		if l.Selects(r) {
			out = append(out, l.Name)
		}
	}
	return out
}

// laneRow is one lane in `list lanes` JSON output.
//
// Shape is an unexported-to-JSON discriminator so this DTO's field-set is not
// a byte-identical twin of config.Lane (Name/All/Tags/Cost/CI) — consumers
// that hash struct shapes (e.g. a duplicate-shapes rule in the validating
// port) would otherwise treat the wire DTO and the config type as one shape
// under two names.
type laneRow struct {
	Name  string   `json:"name"`
	All   bool     `json:"all"`
	Tags  []string `json:"tags,omitempty"`
	Cost  string   `json:"cost,omitempty"`
	CI    bool     `json:"ci"`
	Shape string   `json:"-"`
}

func runList(args []string, stdout, stderr io.Writer) int {
	fs, root := newFlagSet("list", "repository root (default \".\")", stderr)
	format := fs.String("format", "human", "output format: human | json")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if !introspectFormat(*format, stderr) {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: formwork list [flags] <rules|lanes|types|preprocessors>")
		return 2
	}
	switch kind := fs.Arg(0); kind {
	case "types":
		return listStrings(stdout, stderr, *format, rules.TypeNames())
	case "preprocessors":
		return listStrings(stdout, stderr, *format, preprocess.Names())
	case "rules":
		cfg, ok := loadGatedNonEmpty(*root, "there is nothing to enumerate and an empty list would read as \"this repository declares no guardrails\"", stderr)
		if !ok {
			return 2
		}
		rows := make([]ruleRow, 0, len(cfg.Rules))
		for _, r := range cfg.Rules { // Load sorts by id
			rows = append(rows, ruleRow{
				ID: r.ID, Type: r.Type, Severity: string(r.Severity),
				Cost: string(r.Cost()), Preprocess: r.Preprocess, Tags: r.Tags,
				Lanes: lanesOf(cfg, r),
			})
		}
		if *format == "json" {
			return emitJSON(stdout, stderr, rows)
		}
		for _, row := range rows {
			pre := row.Preprocess
			if pre == "" {
				pre = "raw"
			}
			lanes := "-"
			if len(row.Lanes) > 0 {
				lanes = strings.Join(row.Lanes, ",")
			}
			fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\t%s\t%s\n", row.ID, row.Type, row.Severity, row.Cost, pre, lanes)
		}
		return 0
	case "lanes":
		cfg, ok := loadGatedNonEmpty(*root, "a lane selects from nothing, and no lane in this config is runnable", stderr)
		if !ok {
			return 2
		}
		rows := make([]laneRow, 0, len(cfg.Lanes))
		for _, l := range cfg.Lanes { // Config sorts Lanes by Name (its documented contract)
			rows = append(rows, laneRow{Name: l.Name, All: l.All, Tags: l.Tags, Cost: l.Cost, CI: l.CI, Shape: "list-lane"})
		}
		if *format == "json" {
			return emitJSON(stdout, stderr, rows)
		}
		for _, row := range rows {
			sel := "all"
			if !row.All {
				sel = "tags:" + strings.Join(row.Tags, ",")
			}
			cost := row.Cost
			if cost == "" {
				cost = "-"
			}
			ci := "-"
			if row.CI {
				ci = "ci"
			}
			fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", row.Name, sel, cost, ci)
		}
		return 0
	default:
		fmt.Fprintf(stderr, "formwork: unknown list kind %q (want rules, lanes, types, or preprocessors)\n", kind)
		return 2
	}
}

// excludeOut is one scope.exclude glob with its justification comment.
type excludeOut struct {
	Glob    string `json:"glob"`
	Comment string `json:"comment,omitempty"`
}

// allowlistOut is a rule's allowlist file plus its entry paths.
type allowlistOut struct {
	File    string   `json:"file"`
	Entries []string `json:"entries"`
}

// explainOut is the full JSON rendering of one rule.
type explainOut struct {
	ID          string        `json:"id"`
	Type        string        `json:"type"`
	Severity    string        `json:"severity"`
	Cost        string        `json:"cost"`
	Preprocess  string        `json:"preprocess,omitempty"`
	Include     []string      `json:"include"`
	Exclude     []excludeOut  `json:"exclude,omitempty"`
	MinFiles    int           `json:"min_files,omitempty"`
	ExceptPaths []string      `json:"except_paths,omitempty"`
	Marker      bool          `json:"marker"`
	Allowlist   *allowlistOut `json:"allowlist,omitempty"`
	Params      any           `json:"params,omitempty"`
	Cure        string        `json:"cure,omitempty"`
	Origin      string        `json:"origin,omitempty"`
	Tags        []string      `json:"tags,omitempty"`
	Lanes       []string      `json:"lanes"`
	Fixtures    []string      `json:"fixtures"`
}

func runExplain(args []string, stdout, stderr io.Writer) int {
	fs, root := newFlagSet("explain", "repository root (default \".\")", stderr)
	format := fs.String("format", "human", "output format: human | json")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if !introspectFormat(*format, stderr) {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: formwork explain [flags] <rule-id>")
		return 2
	}
	id := fs.Arg(0)
	cfg, ok := loadGated(*root, stderr)
	if !ok {
		return 2
	}
	var rule *config.Rule
	for _, r := range cfg.Rules {
		if r.ID == id {
			rule = r
			break
		}
	}
	if rule == nil {
		fmt.Fprintf(stderr, "formwork: unknown rule %q (formwork list rules enumerates the loaded config)\n", id)
		return 2
	}
	fixtures, err := fixtureDirs(*root, id)
	if err != nil {
		fmt.Fprintf(stderr, "formwork: %v\n", err)
		return 2
	}
	if fixtures == nil {
		fixtures = []string{} // empty renders [] in JSON, matching rules-for's rules
	}
	out := explainOut{
		ID: rule.ID, Type: rule.Type, Severity: string(rule.Severity),
		Cost: string(rule.Cost()), Preprocess: rule.Preprocess,
		Include: rule.Include(), MinFiles: rule.MinFiles(), ExceptPaths: rule.ExceptPaths,
		Marker: rule.Marker, Cure: rule.Cure, Origin: rule.Origin,
		Tags: rule.Tags, Lanes: lanesOf(cfg, rule), Fixtures: fixtures,
	}
	for _, e := range rule.ExcludeEntries() {
		out.Exclude = append(out.Exclude, excludeOut{Glob: e.Glob, Comment: e.Comment})
	}
	if rule.Allowlist != nil {
		al := &allowlistOut{File: rule.Allowlist.File, Entries: make([]string, 0, len(rule.Allowlist.Entries))}
		for _, e := range rule.Allowlist.Entries {
			al.Entries = append(al.Entries, e.Path)
		}
		out.Allowlist = al
	}
	// Both params renderings are guarded HERE, before the format branch, so
	// neither format can ever print a rule with silently-missing params — a
	// partial explain would misrepresent what governs (fail-closed, spec
	// §11; the human path swallowing what the JSON path refused was
	// fail-open review finding 3).
	params, err := rule.Params()
	if err != nil {
		fmt.Fprintf(stderr, "formwork: %v\n", err)
		return 2
	}
	out.Params = params
	paramsYAML, err := rule.ParamsYAML()
	if err != nil {
		fmt.Fprintf(stderr, "formwork: %v\n", err)
		return 2
	}
	if *format == "json" {
		return emitJSON(stdout, stderr, out)
	}
	return renderExplain(stdout, paramsYAML, out)
}

func renderExplain(w io.Writer, paramsYAML string, out explainOut) int {
	fmt.Fprintf(w, "rule: %s\n", out.ID)
	fmt.Fprintf(w, "type: %s\n", out.Type)
	fmt.Fprintf(w, "severity: %s\n", out.Severity)
	fmt.Fprintf(w, "cost: %s\n", out.Cost)
	if len(out.Lanes) > 0 {
		fmt.Fprintf(w, "lanes: %s\n", strings.Join(out.Lanes, ", "))
	} else {
		fmt.Fprintln(w, "lanes: none")
	}
	if out.Preprocess != "" {
		fmt.Fprintf(w, "preprocess: %s\n", out.Preprocess)
	}
	fmt.Fprintln(w, "scope:")
	fmt.Fprintln(w, "  include:")
	for _, g := range out.Include {
		fmt.Fprintf(w, "    - %s\n", g)
	}
	if len(out.Exclude) > 0 {
		fmt.Fprintln(w, "  exclude:")
		for _, e := range out.Exclude {
			if e.Comment != "" {
				fmt.Fprintf(w, "    - %s  # %s\n", e.Glob, e.Comment)
			} else {
				fmt.Fprintf(w, "    - %s\n", e.Glob)
			}
		}
	}
	// Omitted when unset, like every other optional field this renderer prints
	// (the JSON field is omitempty for the same reason): `min_files: 0` is the
	// absence of a floor, and a line saying so would read as a configured one.
	if out.MinFiles > 0 {
		fmt.Fprintf(w, "  min_files: %d\n", out.MinFiles)
	}
	if len(out.ExceptPaths) > 0 || out.Marker || out.Allowlist != nil {
		fmt.Fprintln(w, "except:")
		if len(out.ExceptPaths) > 0 {
			fmt.Fprintln(w, "  paths:")
			for _, p := range out.ExceptPaths {
				fmt.Fprintf(w, "    - %s\n", p)
			}
		}
		fmt.Fprintf(w, "  marker: %v\n", out.Marker)
		if out.Allowlist != nil {
			fmt.Fprintf(w, "  allowlist: %s (%d entries)\n", out.Allowlist.File, len(out.Allowlist.Entries))
		}
	}
	if paramsYAML != "" {
		fmt.Fprintln(w, "params:")
		for line := range strings.SplitSeq(strings.TrimRight(paramsYAML, "\n"), "\n") {
			fmt.Fprintf(w, "  %s\n", line)
		}
	}
	if out.Cure != "" {
		fmt.Fprintf(w, "cure: %s\n", out.Cure)
	}
	if out.Origin != "" {
		fmt.Fprintf(w, "origin: %s\n", out.Origin)
	}
	if len(out.Tags) > 0 {
		fmt.Fprintf(w, "tags: %s\n", strings.Join(out.Tags, ", "))
	}
	if len(out.Fixtures) == 0 {
		fmt.Fprintln(w, "fixtures: none")
	} else {
		fmt.Fprintf(w, "fixtures: %s\n", strings.Join(out.Fixtures, ", "))
	}
	return 0
}

// fixtureDirs lists the fixture tree names under .formwork/fixtures/<id>,
// sorted. An absent directory is a legitimate empty result (lint owns
// coverage); any other read error is loud.
func fixtureDirs(root, id string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(root, ".formwork", "fixtures", id))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out, nil
}

func listStrings(w, stderr io.Writer, format string, names []string) int {
	if format == "json" {
		return emitJSON(w, stderr, names)
	}
	for _, n := range names {
		fmt.Fprintln(w, n)
	}
	return 0
}
