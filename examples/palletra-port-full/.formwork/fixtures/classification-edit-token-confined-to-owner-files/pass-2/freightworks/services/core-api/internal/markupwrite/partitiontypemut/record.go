//go:build ignore

package partitiontypemut

import "github.com/palletra/freightworks/services/core-api/internal/markupwrite/mutbase"

// The sole sanctioned user of the bypass token — this file is on the carve-out.
func RecordAndUpdatePartitionType() {
	auth := mutbase.ClassificationOverride
	_ = auth
}
