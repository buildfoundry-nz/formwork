//go:build ignore

package slots

// pendingMaskCodes are genuinely value-free masks — none is a live PageTallyKey.
var pendingMaskCodes = map[string]struct{}{
	"declared_values": {},
	"review_queued":   {},
}
