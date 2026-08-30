//go:build ignore

package scalewire

// The skip path must never persist scale.StatusUnset (that is a verdict).
func onBypass() scale.Result {
	return scale.Result{Status: scale.StatusExempt}
}
