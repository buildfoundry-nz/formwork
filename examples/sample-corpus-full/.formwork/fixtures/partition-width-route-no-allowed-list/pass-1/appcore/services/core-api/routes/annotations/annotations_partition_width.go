//go:build ignore

package annotations

// Validate against the shared cap, not a local allowlist.
func setPartitionWidth(w int) error {
	return validateAgainstLimit(w, partitionwidth.MaxPartitionWidthMm)
}
