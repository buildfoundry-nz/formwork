//go:build ignore

package priceditems

// Folds onto the shared helper instead of re-typing the tuple tail.
func respond() (int, string, []byte, error) {
	return shared.SuccessProtoResponse(resp)
}
