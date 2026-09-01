package config

import (
	"fmt"
	iofs "io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/buildfoundry-nz/formwork/stdlib"
)

// LoadRules parses the lanes, scope, and .formwork/rules/*.yaml rule files
// against the envelope ReadEnvelope already read — the exact same bytes any
// gate the caller ran between ReadEnvelope and LoadRules evaluated — and
// returns the compiled Config. Library packs named in formwork.yaml load
// first; a local file restating the same id replaces the pack rule.
func (e *Envelope) LoadRules() (*Config, error) {
	lanes, err := compileLanes(e.root.Lanes)
	if err != nil {
		return nil, fmt.Errorf("config: formwork.yaml: %w", err)
	}
	scope, err := compileScope(e.root.Scope)
	if err != nil {
		return nil, fmt.Errorf("config: formwork.yaml: %w", err)
	}
	ignore, err := compileIgnore(e.root.Scan.Ignore)
	if err != nil {
		return nil, fmt.Errorf("config: formwork.yaml: %w", err)
	}
	gitignore, err := compileGitignore(e.root.Scan.Gitignore)
	if err != nil {
		return nil, fmt.Errorf("config: formwork.yaml: %w", err)
	}

	ruleFiles, err := filepath.Glob(filepath.Join(e.dir, "rules", "*.yaml"))
	if err != nil {
		return nil, fmt.Errorf("config: listing rules: %w", err)
	}
	sort.Strings(ruleFiles)

	if err := validateLibraryNames(e.root.Library); err != nil {
		return nil, fmt.Errorf("config: formwork.yaml: %w", err)
	}

	cfg := &Config{Version: e.Version, Engine: e.Engine, EngineConstraint: e.EngineConstraint, Lanes: lanes, Scope: scope, Ignore: ignore, Gitignore: gitignore, Library: append([]string(nil), e.root.Library...), RuleFiles: len(ruleFiles)}
	seen := map[string]string{} // rule id -> file it was defined in
	index := map[string]int{}   // rule id -> slot in cfg.Rules

	for _, pack := range e.root.Library {
		packFS, err := stdlib.Open(pack)
		if err != nil {
			return nil, fmt.Errorf("config: formwork.yaml: library: %w", err)
		}
		matches, err := iofs.Glob(packFS, "*.yaml")
		if err != nil {
			return nil, fmt.Errorf("config: library %s: listing rules: %w", pack, err)
		}
		sort.Strings(matches)
		if len(matches) == 0 {
			return nil, fmt.Errorf("config: library %s: pack contains no rule files", pack)
		}
		for _, name := range matches {
			data, err := iofs.ReadFile(packFS, name)
			if err != nil {
				return nil, fmt.Errorf("config: library %s: reading %s: %w", pack, name, err)
			}
			src := "library:" + pack + "/" + name
			if err := loadRuleFile(cfg, seen, index, src, data, e.dir, pack); err != nil {
				return nil, err
			}
		}
	}

	for _, rf := range ruleFiles {
		data, err := os.ReadFile(rf)
		if err != nil {
			return nil, fmt.Errorf("config: reading %s: %w", rf, err)
		}
		if err := loadRuleFile(cfg, seen, index, rf, data, e.dir, ""); err != nil {
			return nil, err
		}
	}
	sort.Slice(cfg.Rules, func(i, j int) bool { return cfg.Rules[i].ID < cfg.Rules[j].ID })
	return cfg, nil
}

func validateLibraryNames(names []string) error {
	seen := map[string]struct{}{}
	for _, n := range names {
		if !idRE.MatchString(n) {
			return fmt.Errorf("library: name %q must be kebab-case (%s)", n, idRE)
		}
		if _, dup := seen[n]; dup {
			return fmt.Errorf("library: duplicate pack %q", n)
		}
		seen[n] = struct{}{}
	}
	return nil
}

// loadRuleFile compiles every rule in one YAML document. library is the pack
// name when the bytes came from a stdlib pack, empty when they came from the
// adopting repo. Local duplicate ids fail; a local id that already exists on
// a library rule replaces it (local wins).
func loadRuleFile(cfg *Config, seen map[string]string, index map[string]int, src string, data []byte, dir, library string) error {
	var spec fileSpec
	if err := strictUnmarshal(data, &spec); err != nil {
		return fmt.Errorf("config: %s: %w", src, err)
	}
	for _, rs := range spec.Rules {
		if library != "" && rs.Except.Allowlist != "" {
			return fmt.Errorf("config: %s: library rule %s: except.allowlist is repo-local; redeclare the rule locally to bind an allowlist", src, rs.ID)
		}
		rule, err := compile(rs, dir)
		if err != nil {
			return fmt.Errorf("config: %s: %w", src, err)
		}
		rule.Library = library
		if prev, dup := seen[rule.ID]; dup {
			if library == "" && strings.HasPrefix(prev, "library:") {
				cfg.Rules[index[rule.ID]] = rule
				seen[rule.ID] = src
				continue
			}
			return fmt.Errorf("config: %s: duplicate rule id %q (already defined in %s)", src, rule.ID, prev)
		}
		seen[rule.ID] = src
		index[rule.ID] = len(cfg.Rules)
		cfg.Rules = append(cfg.Rules, rule)
	}
	return nil
}
