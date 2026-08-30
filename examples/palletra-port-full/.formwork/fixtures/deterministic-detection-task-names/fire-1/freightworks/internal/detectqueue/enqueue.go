//go:build ignore

package detectqueue

import (
	"fmt"
	"time"
)

// IdentificationTaskName must be pure, but this build folds the clock into the name.
func IdentificationTaskName(sheetID string, epoch int) string { // want: deterministic-detection-task-names
	return fmt.Sprintf("detect-%s-%d-%d", sheetID, epoch, time.Now().Unix())
}

// markCreatedAt legitimately reads the clock — it is not a task-name builder,
// so the body-scoped ban must not fire here.
func markCreatedAt() int64 {
	return time.Now().Unix()
}
