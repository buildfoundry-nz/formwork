//go:build ignore

package detection

// classToTypePartitions silently coerces every unknown Visionkit class into a valid-
// looking annotation row via a catch-all init — the #5912 corruption vector.
func classToTypePartitions(class string) string {
	partitionType := "internal_partitions" // want: no-catchall-default-for-class-to-type
	return partitionType
}
