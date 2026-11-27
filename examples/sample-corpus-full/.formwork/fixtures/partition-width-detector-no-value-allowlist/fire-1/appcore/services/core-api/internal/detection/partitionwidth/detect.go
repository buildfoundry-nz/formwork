//go:build ignore

package partitionwidth

// Regression: a hardcoded value allowlist replaces the range cap.
var PresetWidths = map[string]bool{"90": true, "140": true} // want: partition-width-detector-no-value-allowlist
