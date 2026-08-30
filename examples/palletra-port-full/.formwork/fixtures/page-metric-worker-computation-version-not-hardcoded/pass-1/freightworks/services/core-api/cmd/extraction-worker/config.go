//go:build ignore

package main

import "github.com/palletra/freightworks/services/core-api/internal/parsewrite"

func defaults(cfg *Config) {
	cfg.DerivationVersion = parsewrite.DefaultAnalysisVersion
}
