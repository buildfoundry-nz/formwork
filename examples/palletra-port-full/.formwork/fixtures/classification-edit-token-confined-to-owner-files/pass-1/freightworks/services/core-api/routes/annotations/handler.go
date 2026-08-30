//go:build ignore

package annotations

import "github.com/palletra/freightworks/services/core-api/internal/markupwrite/partitiontypemut"

func handle() {
	_ = partitiontypemut.RecordAndUpdatePartitionType
}
