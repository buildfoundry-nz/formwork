// Package corpus is the public API for analysing a formwork rule corpus.
//
// WHY THIS EXISTS. A tool that asks questions ABOUT a corpus — can this rule
// ever fail, does this rule still fire, does this arm match anything — has to
// measure scope with the SAME scanner and matcher the gate engine runs.
// Re-implementing doublestar matching is not a shortcut, it is the known way to
// get a wrong answer: a downstream census that did so reported "133 of 200
// rules match zero files", and every one of those was an artefact of the
// re-implementation.
//
// Those parts live under internal/, so Go admits only importers whose module
// path sits under this module's. Downstream analysers reached them by declaring
// module paths like github.com/buildfoundry-nz/formwork/vacuitycensus from a
// different repository entirely — Go's internal rule keys on the import-path
// prefix, so a module can grant itself access by naming itself into another
// module's path. Three such modules, some 8,000 lines, depend on that today.
//
// That arrangement works only because Go checks a string prefix rather than
// actual module membership, and it is invisible from here: a rename under
// internal/ breaks a build in a repository this one has never heard of. This
// package replaces the workaround with a supported surface, so an analyser can
// be an ordinary consumer of a published module (buildfoundry-nz/formwork#5).
//
// The types are aliases, not wrappers. A *Rule obtained here IS the engine's
// rule, so nothing is lost across the boundary and there is no conversion cost.
package corpus

import (
	"github.com/buildfoundry-nz/formwork/internal/config"
	"github.com/buildfoundry-nz/formwork/internal/engine"
	"github.com/buildfoundry-nz/formwork/internal/finding"
	"github.com/buildfoundry-nz/formwork/internal/rules"
	"github.com/buildfoundry-nz/formwork/internal/scan"

	// Registering every built-in rule type is the point of importing this
	// package rather than the pieces: an analyser that misses one silently
	// measures a corpus the engine would have judged differently. Downstream
	// carried 47 blank imports to do this by hand.
	_ "github.com/buildfoundry-nz/formwork/internal/rules/baseline"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/binarycontent"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/command"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/dartscan"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/docpathexists"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/filenaming"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/filesize"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/gitdiff"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/goast"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/ordering"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/pairconsistency"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/pattern"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/patterncount"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/setrelation"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/sqlparse"
	_ "github.com/buildfoundry-nz/formwork/internal/rules/sqltext"
)

// The corpus: rules as the engine loads and validates them.
type (
	// Config is a loaded .formwork/ corpus.
	Config = config.Config
	// Rule is one rule, exactly as the engine evaluates it.
	Rule = config.Rule
)

// Load reads and validates the corpus under repoRoot/.formwork.
func Load(repoRoot string) (*Config, error) { return config.Load(repoRoot) }

// New builds a rule in memory, for analysers that synthesise one to probe with.
func New(id, typeName string, sev Severity, cure string, include, exclude, exceptPaths []string, c Checker) (*Rule, error) {
	return config.New(id, typeName, sev, cure, include, exclude, exceptPaths, c)
}

// The tree: the same scan the gate runs, so scope is measured identically.
type (
	// File is one scanned file.
	File = scan.File
	// FileSet is a whole scanned tree.
	FileSet = scan.FileSet
)

// Walk scans a tree the way the engine does.
func Walk(root string) (*FileSet, error) { return scan.Walk(root) }

// NewMemFile makes an in-memory file, for probing a rule without touching disk.
func NewMemFile(relPath string, content []byte) *File { return scan.NewMemFile(relPath, content) }

// UnderBuiltinSkip reports whether a path sits under a directory the engine
// always skips. An analyser that does not ask will count files the gate cannot
// see, and report a rule as covering them.
func UnderBuiltinSkip(path string) bool { return scan.UnderBuiltinSkip(path) }

// Evaluation: findings, exactly as the gate produces them.
type (
	// Finding is one rule violation.
	Finding = finding.Finding
	// Severity is a finding's severity.
	Severity = finding.Severity
)

// SeverityError is the severity that fails a gate.
const SeverityError = finding.SeverityError

// Run evaluates rules over a file set, as the gate does.
func Run(rls []*Rule, fset *FileSet, workers int) ([]Finding, error) {
	return engine.Run(rls, fset, workers)
}

// Unsuppressed drops findings an allowlist has silenced. An analyser asking
// whether a rule CAN fail wants the unsuppressed set: a rule whose every
// finding is allowlisted is not a rule that fires.
func Unsuppressed(fs []Finding) []Finding { return finding.Unsuppressed(fs) }

// Rule types: what a rule is made of, for analysers that classify by cost or
// resolve a type name.
type (
	// Checker is a constructed rule type.
	Checker = rules.Checker
	// Cost says how expensive a checker is to run.
	Cost = rules.Cost
	// Factory builds a Checker from a rule's params.
	Factory = rules.Factory
	// AnchorProbe is the fail-closed half of a NAME-anchored rule type.
	AnchorProbe = rules.AnchorProbe
)

// CostHeavy marks a checker that shells out or is otherwise expensive.
const CostHeavy = rules.CostHeavy

// CostOf reports a checker's cost.
func CostOf(c Checker) Cost { return rules.CostOf(c) }

// Lookup resolves a rule type name to its factory. Reports false for a name no
// built-in registers — which, because this package registers them all, means
// the name is genuinely unknown rather than merely unimported.
func Lookup(name string) (Factory, bool) { return rules.Lookup(name) }
