// Package config loads .formwork/ YAML strictly and compiles rule envelopes
// into executable rules (spec §5). Any schema error is fatal — never a
// silent skip.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/bmatcuk/doublestar/v4"
	"github.com/buildfoundry-nz/formwork/internal/finding"
	"github.com/buildfoundry-nz/formwork/internal/preprocess"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	"gopkg.in/yaml.v3"
)

// Rule is a compiled rule: envelope plus instantiated checker.
type Rule struct {
	ID       string
	Type     string
	Severity finding.Severity
	Cure     string
	Origin   string
	Tags     []string
	Checker  rules.Checker

	// FixtureExempt is a heavy rule's declared reason for carrying no fixtures
	// (#53). Empty means undeclared, which lint reports rather than skips.
	FixtureExempt string

	Preprocess  string     // preprocess variant name; "" means raw
	Marker      bool       // honor inline formwork:allow markers
	Allowlist   *Allowlist // loaded except.allowlist, nil when absent
	ExceptPaths []string   // except.paths carve-out globs (read-only after construction)

	// Library is the pack name this rule was loaded from (e.g. "generic").
	// Empty means the rule was declared in the adopting repo. Lint's
	// fixture-coverage check skips library-sourced rules: their fixtures
	// live in the pack and are proven by `formwork test -C stdlib/<name>`.
	Library string

	include  []string
	exclude  []string
	minFiles int
	// excludeComments is parallel to exclude: the YAML head/line comment on
	// each scope.exclude entry (trimmed, without the leading '#'), empty when
	// the entry had no comment. Lint uses it to require a justification on
	// dead excludes without banning legitimate preventative ones.
	excludeComments []string
	factory         rules.Factory
	params          *yaml.Node
}

// ExcludeEntry is one scope.exclude glob plus its optional YAML justification
// comment (empty when the entry had none).
type ExcludeEntry struct {
	Glob    string
	Comment string
}

// Allowlist is a loaded exemption file: exact repo-relative paths whose
// findings for the owning rule are suppressed (phase-3a design §3).
type Allowlist struct {
	File    string // as configured, relative to .formwork/
	Entries []AllowlistEntry
}

// AllowlistEntry is one path plus its 1-based line in the allowlist file
// (used by lint staleness messages).
type AllowlistEntry struct {
	Path string
	Line int
}

// New builds a compiled rule directly. Load uses it; engine and report tests
// use it to construct rules without YAML.
func New(id, typeName string, sev finding.Severity, cure string, include, exclude, exceptPaths []string, c rules.Checker) (*Rule, error) {
	if len(include) == 0 {
		return nil, fmt.Errorf("rule %s: scope.include must not be empty", id)
	}
	for _, globs := range [][]string{include, exclude, exceptPaths} {
		for _, g := range globs {
			if !doublestar.ValidatePattern(g) {
				return nil, fmt.Errorf("rule %s: invalid glob %q", id, g)
			}
		}
	}
	return &Rule{
		ID: id, Type: typeName, Severity: sev, Cure: cure,
		include: include, exclude: exclude, ExceptPaths: exceptPaths, Checker: c,
	}, nil
}

// Applies reports whether relPath (slash-separated) is in this rule's scope.
func (r *Rule) Applies(relPath string) bool {
	return matchAny(r.include, relPath) &&
		!matchAny(r.exclude, relPath) &&
		!matchAny(r.ExceptPaths, relPath)
}

// CarvedOutBy reports which except.paths glob removed relPath from this rule's
// evaluation, and whether any did.
//
// The include/exclude test comes FIRST and is the whole point: a path the rule
// never covered was not carved out by anything, so a count built on this
// measures a carve-out's EFFECT rather than its declaration (#138). That is the
// same distinction scan.ignore already reports with live match counts, and the
// one except.paths was missing — an entry whose subject moved away printed with
// the same weight as one doing real work.
//
// except.paths is a scope SUBTRACTION, not a suppression: Applies returns false
// for a matched path, so the rule never evaluates the file and there is no
// finding for a census to count. Reading the file set is the only way to see it.
func (r *Rule) CarvedOutBy(relPath string) (string, bool) {
	if !matchAny(r.include, relPath) || matchAny(r.exclude, relPath) {
		return "", false
	}
	for _, g := range r.ExceptPaths {
		if ok, err := doublestar.Match(g, relPath); err == nil && ok {
			return g, true
		}
	}
	return "", false
}

// Exclude returns this rule's scope.exclude globs (read-only; do not mutate).
// Empty when the rule has no excludes. Used by formwork lint's escape-hatch
// census and dead-exclude hygiene.
func (r *Rule) Exclude() []string {
	return r.exclude
}

// ExcludeEntries returns each scope.exclude glob paired with its YAML
// justification comment (empty string when the entry had none).
func (r *Rule) ExcludeEntries() []ExcludeEntry {
	out := make([]ExcludeEntry, len(r.exclude))
	for i, g := range r.exclude {
		c := ""
		if i < len(r.excludeComments) {
			c = r.excludeComments[i]
		}
		out[i] = ExcludeEntry{Glob: g, Comment: c}
	}
	return out
}

// MinFiles returns this rule's scope floor: the number of files its scope must
// select before the rule is considered to have had anything to look at. Zero —
// the default for every rule that does not declare `scope.min_files` — means no
// floor, which is exactly the behaviour that shipped before the key existed.
func (r *Rule) MinFiles() int {
	return r.minFiles
}

// SetMinFiles sets the scope floor, refusing a value that could never fire.
//
// The field is unexported and this is the only door onto it, so compile (the
// YAML path) and the New-plus-setter path used by tests cannot disagree about
// what a legal floor is. A negative floor is refused rather than clamped: every
// possible file count satisfies it, so it would read as armed while being
// incapable of failing.
func (r *Rule) SetMinFiles(n int) error {
	if n < 0 {
		return fmt.Errorf("rule %s: scope.min_files must be >= 0, got %d (a negative floor can never fire)", r.ID, n)
	}
	r.minFiles = n
	return nil
}

// Include returns this rule's scope.include globs (read-only; do not mutate).
// Introspection surface (#105/#108): explain and rules-for render scope from
// the same globs Applies consults, so the display can never drift from the
// verdict.
func (r *Rule) Include() []string {
	return r.include
}

// Params returns the rule's retained params decoded into plain Go values
// (maps/slices/scalars), or nil when the rule declared none. Introspection
// only — the checker was compiled from this same node at load time, so this
// is a rendering of what already governs, never a second source of truth.
func (r *Rule) Params() (any, error) {
	if r.params == nil || r.params.Kind == 0 {
		return nil, nil
	}
	var v any
	if err := r.params.Decode(&v); err != nil {
		return nil, fmt.Errorf("rule %s: decoding params: %w", r.ID, err)
	}
	return v, nil
}

// ParamsYAML re-renders the retained params node as YAML for human display,
// or "" when the rule declared none.
func (r *Rule) ParamsYAML() (string, error) {
	if r.params == nil || r.params.Kind == 0 {
		return "", nil
	}
	out, err := yaml.Marshal(r.params)
	if err != nil {
		return "", fmt.Errorf("rule %s: rendering params: %w", r.ID, err)
	}
	return string(out), nil
}

// Cost is the rule's evaluation cost class, defaulting to fast for checkers
// that do not declare one (spec §8).
func (r *Rule) Cost() rules.Cost {
	return rules.CostOf(r.Checker)
}

// WholeTreeInvariant reports whether the rule's verdict is a whole-repo
// invariant that must be evaluated over the whole tree even under a
// --staged/--range changeset scan (mirrors Cost). Range-scoping a required-
// pattern(exists) / set-relation / pattern-count / baseline rule false-fails it
// when the file bearing its token is not in the change range (#4).
func (r *Rule) WholeTreeInvariant() bool {
	return rules.IsWholeTreeInvariant(r.Checker)
}

// Fresh returns a copy of the rule with a newly constructed checker.
// Checkers may carry per-run state (e.g. required-pattern exists mode), so
// evaluating a rule against multiple independent trees — as the fixture
// runner does — requires a fresh instance per tree. Rules built with New
// (no factory) cannot be refreshed. The clone is shallow and shares the
// scope slices and params node with the original; this is safe because both
// are treated as read-only after construction.
func (r *Rule) Fresh() (*Rule, error) {
	if r.factory == nil {
		return nil, fmt.Errorf("rule %s: cannot refresh a rule constructed without a factory", r.ID)
	}
	checker, err := r.factory(r.params)
	if err != nil {
		return nil, fmt.Errorf("rule %s: %w", r.ID, err)
	}
	return r.CloneWithChecker(checker), nil
}

// CloneWithChecker returns a shallow copy of the rule with its checker
// replaced. Used by lint's load-bearing-prefilter differential to evaluate a
// prefilter-stripped variant of a rule. The clone shares the scope slices,
// params node, and allowlist with the original; all are read-only after
// construction, so sharing is safe (mirrors Fresh).
func (r *Rule) CloneWithChecker(c rules.Checker) *Rule {
	clone := *r
	clone.Checker = c
	return &clone
}

func matchAny(patterns []string, path string) bool {
	for _, p := range patterns {
		// Match's only error is ErrBadPattern, which New's ValidatePattern
		// call has already ruled out for every stored glob — the error branch
		// is unreachable here, not swallowed.
		if ok, err := doublestar.Match(p, path); err == nil && ok {
			return true
		}
	}
	return false
}

// Lane is an orchestration group: a named selector over rules (spec §8).
// Exactly one of All or Tags is set (validated at load). Cost, when set,
// further restricts the lane to rules of that cost class ("" = any cost). CI
// reports whether the lane runs in CI — lint's lane-reachability check
// (spec §9) requires every rule to be selected by at least one CI lane.
type Lane struct {
	Name string
	All  bool
	Tags []string
	Cost string // "", "fast", or "heavy"
	CI   bool
}

// Selects reports whether this lane runs r: the cost filter (if any) must
// match, then an all-lane selects every remaining rule and a tag lane selects
// a rule sharing at least one tag.
func (l Lane) Selects(r *Rule) bool {
	if l.Cost != "" && l.Cost != string(r.Cost()) {
		return false
	}
	if l.All {
		return true
	}
	for _, lt := range l.Tags {
		for _, rt := range r.Tags {
			if lt == rt {
				return true
			}
		}
	}
	return false
}

// Config is the loaded, compiled configuration. Rules are sorted by ID and
// Lanes by Name.
type Config struct {
	Version int
	// Engine is the raw engine-version constraint from formwork.yaml ("" if
	// absent). EngineConstraint is its parsed form (nil if absent). A binary
	// whose version does not satisfy the constraint refuses to run (spec §4).
	Engine           string
	EngineConstraint *semver.Constraints
	Lanes            []Lane
	Scope            ScopeConfig
	Ignore           []IgnoreEntry
	// Gitignore is the scan.gitignore declaration, nil when the key is absent.
	// Absent is off: a repo that does not declare it gets exactly the walk it
	// got before this key existed.
	Gitignore *GitignoreEntry
	Rules     []*Rule
	// Library is the pack names declared in formwork.yaml, in declaration
	// order. Empty when the key is absent or the list is empty.
	Library []string
	// RuleFiles is how many .formwork/rules/*.yaml files were read, whatever
	// they declared. It exists so a zero-rule refusal can name the actual
	// cause: no rule files at all is a different mistake from rule files that
	// parse and declare nothing (an empty or null `rules:` list, the shape a
	// bad merge or a templating error produces), and "add a rule file" is
	// useless advice to someone staring at the rule file they just wrote.
	RuleFiles int
}

// ScopeConfig classifies a changeset (spec §8): which paths are docs,
// governance, or (by default) runtime, plus per-language change globs.
type ScopeConfig struct {
	Docs       []string
	Governance []string
	Languages  []LangClass // sorted by Name for deterministic output
}

// LangClass maps a language flag name to the globs that trigger it.
type LangClass struct {
	Name  string
	Globs []string
}

var scopeRanks = []string{"docs", "governance", "runtime"}

// Classify returns the changeset's class and per-language change flags. A file
// matching no docs/governance glob is runtime (fail-closed, spec §11); the
// changeset takes the highest class of any file (runtime > governance > docs).
// An empty changeset is docs (nothing to gate).
func (sc ScopeConfig) Classify(changed []string) (class string, langs map[string]bool) {
	langs = make(map[string]bool, len(sc.Languages))
	for _, l := range sc.Languages {
		langs[l.Name] = false
	}
	best := 0 // index into scopeRanks
	for _, p := range changed {
		fc := 2 // runtime (default / unclassified)
		switch {
		case matchAny(sc.Governance, p):
			fc = 1
		case matchAny(sc.Docs, p):
			fc = 0
		}
		if fc > best {
			best = fc
		}
		for _, l := range sc.Languages {
			if matchAny(l.Globs, p) {
				langs[l.Name] = true
			}
		}
	}
	return scopeRanks[best], langs
}

// Lane returns the named lane and whether it exists.
func (c *Config) Lane(name string) (Lane, bool) {
	for _, l := range c.Lanes {
		if l.Name == name {
			return l, true
		}
	}
	return Lane{}, false
}

// RulesForLane returns the rules the named lane selects, preserving the
// ID-sorted order of Config.Rules. An unknown lane name is an error.
func (c *Config) RulesForLane(name string) ([]*Rule, error) {
	lane, ok := c.Lane(name)
	if !ok {
		return nil, fmt.Errorf("unknown lane %q", name)
	}
	var out []*Rule
	for _, r := range c.Rules {
		if lane.Selects(r) {
			out = append(out, r)
		}
	}
	return out, nil
}

type rootSpec struct {
	Version int                 `yaml:"version"`
	Engine  string              `yaml:"engine"`
	Library []string            `yaml:"library"`
	Lanes   map[string]laneSpec `yaml:"lanes"`
	Scope   scopeConfigSpec     `yaml:"scope"`
	Scan    scanSpec            `yaml:"scan"`
}

type laneSpec struct {
	All  bool     `yaml:"all"`
	Tags []string `yaml:"tags"`
	Cost string   `yaml:"cost"`
	CI   bool     `yaml:"ci"`
}

type scopeConfigSpec struct {
	Docs       []string            `yaml:"docs"`
	Governance []string            `yaml:"governance"`
	Languages  map[string][]string `yaml:"languages"`
}

type fileSpec struct {
	Rules []ruleSpec `yaml:"rules"`
}

type ruleSpec struct {
	ID         string     `yaml:"id"`
	Type       string     `yaml:"type"`
	Severity   string     `yaml:"severity"`
	Scope      scopeSpec  `yaml:"scope"`
	Preprocess string     `yaml:"preprocess"`
	Params     yaml.Node  `yaml:"params"`
	Except     exceptSpec `yaml:"except"`
	Cure       string     `yaml:"cure"`
	Origin     string     `yaml:"origin"`
	Tags       []string   `yaml:"tags"`
	// FixtureExempt is a heavy rule's DECLARED reason for carrying no
	// fixtures (#53). Heavy rules used to be exempt by cost, which made
	// "cannot be fixtured by construction" and "nobody bothered" the same
	// state — and command rules are the escape hatch used for the
	// highest-stakes lockdowns, so those were the rules with no firing proof.
	FixtureExempt string `yaml:"fixture_exempt"`
}

type scopeSpec struct {
	Include []string `yaml:"include"`
	// Exclude is a raw node so load can recover per-entry head/line comments
	// (justification for dead-exclude hygiene). A plain []string would drop them.
	Exclude yaml.Node `yaml:"exclude"`
	// MinFiles is a raw node, not an int, because yaml.v3 decoding into int
	// COERCES rather than refusing: measured, `1.5` decodes to 1 and `2.0` to 2
	// with a nil error. parseMinFiles (scopeparse.go) refuses both instead, and
	// TestLoadRejectsBadScopeMinFiles carries a case for each spelling. A floor
	// is a number an operator has to reason about, and silently reading a
	// different number than the one written is the wrong way to be lenient.
	MinFiles yaml.Node `yaml:"min_files"`
}

type exceptSpec struct {
	Paths     []string `yaml:"paths"`
	Marker    bool     `yaml:"marker"`
	Allowlist string   `yaml:"allowlist"`
}

var idRE = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func strictUnmarshal(data []byte, out any) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(out); err != nil {
		return err
	}
	return nil
}

// Envelope is a read-once .formwork/formwork.yaml: the raw bytes are read
// and parsed exactly once, so a caller that needs to gate on it (the
// engine-version backstop, spec §4) before parsing any rule file is
// guaranteed to gate on the very same bytes LoadRules subsequently compiles
// — not a possibly-different later read of the file (finding 8: the file
// used to be read once for the gate via LoadRootMeta and a second time for
// execution via Load, so the gate's guarantee weakened from "allowed to
// evaluate THIS config" to "was allowed to evaluate some recent config").
type Envelope struct {
	dir  string
	root rootSpec

	Version          int
	Engine           string
	EngineConstraint *semver.Constraints
}

// ReadEnvelope reads and validates ONLY .formwork/formwork.yaml, once. The
// caller gates on the envelope (engine constraint) before calling LoadRules,
// so an unsupported binary reports its version rather than failing on rule
// schema it was never built to understand.
func ReadEnvelope(repoRoot string) (*Envelope, error) {
	dir := filepath.Join(repoRoot, ".formwork")
	rootBytes, err := os.ReadFile(filepath.Join(dir, "formwork.yaml"))
	if err != nil {
		return nil, fmt.Errorf("config: reading formwork.yaml: %w", err)
	}
	var root rootSpec
	if err := strictUnmarshal(rootBytes, &root); err != nil {
		return nil, fmt.Errorf("config: formwork.yaml: %w", err)
	}
	if root.Version != 1 {
		return nil, fmt.Errorf("config: formwork.yaml: unsupported version %d (this binary supports version 1)", root.Version)
	}
	var engineConstraint *semver.Constraints
	if root.Engine != "" {
		engineConstraint, err = semver.NewConstraint(root.Engine)
		if err != nil {
			return nil, fmt.Errorf("config: formwork.yaml: engine: invalid constraint %q: %w", root.Engine, err)
		}
	}
	return &Envelope{dir: dir, root: root, Version: root.Version, Engine: root.Engine, EngineConstraint: engineConstraint}, nil
}

// Load reads .formwork/formwork.yaml and .formwork/rules/*.yaml under
// repoRoot and returns compiled rules sorted by ID. A thin wrapper over
// ReadEnvelope + LoadRules for callers (and tests) that have no gate to run
// in between and so don't need the two steps split apart.
func Load(repoRoot string) (*Config, error) {
	env, err := ReadEnvelope(repoRoot)
	if err != nil {
		return nil, err
	}
	return env.LoadRules()
}

// compileLanes validates the lane map from formwork.yaml. A lane must declare
// exactly one selector — all:true or a non-empty tags list — and a kebab-case
// name; anything else is a config error (exit 2). Names are visited in sorted
// order so that, with multiple invalid lanes, the reported error is
// deterministic (mirroring the sorted rule-file handling in Load); the
// returned slice is therefore already Name-sorted.
func compileLanes(specs map[string]laneSpec) ([]Lane, error) {
	names := make([]string, 0, len(specs))
	for name := range specs {
		names = append(names, name)
	}
	sort.Strings(names)

	lanes := make([]Lane, 0, len(specs))
	for _, name := range names {
		spec := specs[name]
		if !idRE.MatchString(name) {
			return nil, fmt.Errorf("lane %s: name must be kebab-case (%s)", name, idRE)
		}
		if spec.Cost != "" && !rules.ValidCost(spec.Cost) {
			return nil, fmt.Errorf("lane %s: invalid cost %q (want fast or heavy)", name, spec.Cost)
		}
		hasTags := len(spec.Tags) > 0
		// Exactly one selector: all XOR tags. spec.All == hasTags is true when
		// both are set (both selectors) or neither is (no selector) — both are
		// config errors.
		if spec.All == hasTags {
			if spec.All {
				return nil, fmt.Errorf("lane %s: declare exactly one selector — all:true or tags:[…], not both", name)
			}
			return nil, fmt.Errorf("lane %s: declare exactly one selector — all:true or a non-empty tags list", name)
		}
		lanes = append(lanes, Lane{Name: name, All: spec.All, Tags: spec.Tags, Cost: spec.Cost, CI: spec.CI})
	}
	return lanes, nil
}

// compileScope validates the scope classifier globs and returns a ScopeConfig
// with languages sorted by name for deterministic output. An empty scope
// section is valid (every change classifies as runtime — the safe default).
func compileScope(spec scopeConfigSpec) (ScopeConfig, error) {
	check := func(field string, globs []string) error {
		for _, g := range globs {
			if !doublestar.ValidatePattern(g) {
				return fmt.Errorf("scope.%s: invalid glob %q", field, g)
			}
		}
		return nil
	}
	if err := check("docs", spec.Docs); err != nil {
		return ScopeConfig{}, err
	}
	if err := check("governance", spec.Governance); err != nil {
		return ScopeConfig{}, err
	}
	sc := ScopeConfig{Docs: spec.Docs, Governance: spec.Governance}
	names := make([]string, 0, len(spec.Languages))
	for name := range spec.Languages {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !idRE.MatchString(name) {
			return ScopeConfig{}, fmt.Errorf("scope.languages: name %q must be kebab-case", name)
		}
		if err := check("languages."+name, spec.Languages[name]); err != nil {
			return ScopeConfig{}, err
		}
		sc.Languages = append(sc.Languages, LangClass{Name: name, Globs: spec.Languages[name]})
	}
	return sc, nil
}

func compile(spec ruleSpec, dir string) (*Rule, error) {
	if spec.ID == "" {
		return nil, errors.New("rule with empty id")
	}
	if !idRE.MatchString(spec.ID) {
		return nil, fmt.Errorf("rule %s: id must be kebab-case (%s)", spec.ID, idRE)
	}
	sev := finding.SeverityError
	switch spec.Severity {
	case "", string(finding.SeverityError):
	case string(finding.SeverityWarn):
		sev = finding.SeverityWarn
	default:
		return nil, fmt.Errorf("rule %s: invalid severity %q (want error or warn)", spec.ID, spec.Severity)
	}
	if _, ok := preprocess.Lookup(spec.Preprocess); !ok {
		return nil, fmt.Errorf("rule %s: unknown preprocessor %q (known: %v)", spec.ID, spec.Preprocess, preprocess.Names())
	}
	factory, ok := rules.Lookup(spec.Type)
	if !ok {
		return nil, fmt.Errorf("rule %s: unknown type %q (known: %v)", spec.ID, spec.Type, rules.TypeNames())
	}
	var params *yaml.Node
	if spec.Params.Kind != 0 {
		params = &spec.Params
	}
	checker, err := factory(params)
	if err != nil {
		return nil, fmt.Errorf("rule %s: %w", spec.ID, err)
	}
	excludeGlobs, excludeComments, err := parseExcludeEntries(spec.Scope.Exclude)
	if err != nil {
		return nil, fmt.Errorf("rule %s: scope.exclude: %w", spec.ID, err)
	}
	minFiles, err := parseMinFiles(spec.Scope.MinFiles)
	if err != nil {
		return nil, fmt.Errorf("rule %s: scope.min_files: %w", spec.ID, err)
	}
	rule, err := New(spec.ID, spec.Type, sev, spec.Cure, spec.Scope.Include, excludeGlobs, spec.Except.Paths, checker)
	if err != nil {
		return nil, err
	}
	if err := rule.SetMinFiles(minFiles); err != nil {
		return nil, err
	}
	rule.excludeComments = excludeComments
	rule.Origin = spec.Origin
	rule.Tags = spec.Tags
	if rule.FixtureExempt, err = fixtureExemptReason(spec.ID, spec.FixtureExempt); err != nil {
		return nil, err
	}
	rule.factory = factory
	rule.params = params
	rule.Preprocess = spec.Preprocess
	rule.Marker = spec.Except.Marker
	if spec.Except.Allowlist != "" {
		al, err := loadAllowlist(dir, spec.Except.Allowlist)
		if err != nil {
			return nil, fmt.Errorf("rule %s: %w", spec.ID, err)
		}
		rule.Allowlist = al
	}
	return rule, nil
}

// loadAllowlist reads an except.allowlist file (path relative to .formwork/):
// one exact repo-relative path per line, # comments and blank lines ignored.
// A missing or unreadable file is a config error — exit 2, never a silent
// no-exemptions fallback.
func loadAllowlist(dir, rel string) (*Allowlist, error) {
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		return nil, fmt.Errorf("allowlist %s: %w", rel, err)
	}
	al := &Allowlist{File: rel}
	for i, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		al.Entries = append(al.Entries, AllowlistEntry{Path: line, Line: i + 1})
	}
	return al, nil
}
