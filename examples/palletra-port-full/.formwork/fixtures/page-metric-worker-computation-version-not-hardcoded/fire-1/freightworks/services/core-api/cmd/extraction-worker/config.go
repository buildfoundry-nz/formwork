//go:build ignore

package main

func defaults(cfg *Config) {
	cfg.DerivationVersion = "v1" // want: page-metric-worker-computation-version-not-hardcoded
}
