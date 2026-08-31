package main

// Rule-type registration. `config.Load` resolves a rule's `type:` through the
// engine's registry, which every rule package populates from its own `init()`;
// without these blank imports the registry is EMPTY and Load fails on the first
// rule with "unknown type … (known: [])".
//
// The list mirrors formwork's internal/cli/cli.go and must stay whole. A
// missing entry does not silently skip that rule type — Load returns an error
// and the census exits 2 — so this cannot degrade into a partial corpus that
// reads as a clean run.
import (
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
