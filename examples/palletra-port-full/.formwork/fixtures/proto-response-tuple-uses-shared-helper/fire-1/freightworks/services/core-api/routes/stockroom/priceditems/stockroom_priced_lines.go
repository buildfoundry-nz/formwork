//go:build ignore

package priceditems

// A hand-rolled raw response tuple that should fold onto the shared helper.
func respond() (int, string, []byte, error) {
	return http.StatusOK, "application/json", respBody, nil // want: proto-response-tuple-uses-shared-helper
}
