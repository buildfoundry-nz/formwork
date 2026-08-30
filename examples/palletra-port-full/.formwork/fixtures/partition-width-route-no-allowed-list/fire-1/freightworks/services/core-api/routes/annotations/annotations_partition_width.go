//go:build ignore

package annotations

// Regression: reintroduces the manual-set value allowlist.
var allowedPartitionWidthsMm = []int{90, 140} // want: partition-width-route-no-allowed-list
