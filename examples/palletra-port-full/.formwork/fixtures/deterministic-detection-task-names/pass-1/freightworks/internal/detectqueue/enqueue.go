//go:build ignore

package detectqueue

import (
	"fmt"
	"time"
)

// IdentificationTaskName reads only its arguments — deterministic dedupe key.
func IdentificationTaskName(sheetID string, epoch int) string {
	return fmt.Sprintf("detect-%s-%d", sheetID, epoch)
}

func PagePaintTaskName(sheetID string, epoch int) string {
	return fmt.Sprintf("render-%s-%d", sheetID, epoch)
}

func WholeExtractionTaskName(projectID string) string {
	return fmt.Sprintf("full-extract-%s", projectID)
}

// The clock lives outside the name builders, which is allowed.
func markCreatedAt() int64 {
	return time.Now().Unix()
}
