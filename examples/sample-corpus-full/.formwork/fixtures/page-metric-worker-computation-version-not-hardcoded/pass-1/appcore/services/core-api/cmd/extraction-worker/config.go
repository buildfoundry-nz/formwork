//go:build ignore

package main

import "github.com/sample/appcore/services/core-api/internal/parsewrite"

func defaults(cfg *Config) {
	cfg.DerivationVersion = parsewrite.DefaultAnalysisVersion
}
