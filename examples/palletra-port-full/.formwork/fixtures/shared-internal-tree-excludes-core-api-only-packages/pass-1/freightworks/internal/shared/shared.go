//go:build ignore

package shared

// Genuinely shared: imported by a non-core-api binary as well as core-api.
func Value() int { return 42 }
