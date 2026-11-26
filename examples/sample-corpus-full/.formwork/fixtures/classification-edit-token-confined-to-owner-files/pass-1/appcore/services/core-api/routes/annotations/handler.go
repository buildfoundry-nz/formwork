//go:build ignore

package annotations

import "github.com/sample/appcore/services/core-api/internal/markupwrite/partitiontypemut"

func handle() {
	_ = partitiontypemut.RecordAndUpdatePartitionType
}
