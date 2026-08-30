//go:build ignore

package scalewire

func onBypass() scale.Result {
	return scale.Result{Status: scale.StatusUnset} // want: drawingquality-scale-gate-no-status-none
}
