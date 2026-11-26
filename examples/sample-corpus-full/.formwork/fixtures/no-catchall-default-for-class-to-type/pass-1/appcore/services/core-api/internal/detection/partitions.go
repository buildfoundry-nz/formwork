//go:build ignore

package detection

// classToTypePartitions returns "" for unknown classes; the caller drops the
// prediction with slog.WarnContext instead of persisting a coerced default.
func classToTypePartitions(class string) string {
	switch class {
	case "external_partitions":
		return "external_partitions"
	case "internal_partitions":
		return "internal_partitions"
	}
	return ""
}
