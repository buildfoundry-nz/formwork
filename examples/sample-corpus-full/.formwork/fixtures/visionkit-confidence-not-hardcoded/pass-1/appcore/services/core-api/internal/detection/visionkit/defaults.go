//go:build ignore

package visionkit

// withDefaults injects the per-family confidence/overlap from config.
func withDefaults(req *ScanRequest, cfg Config) {
	req.Certainty = cfg.Certainty
	req.Coverage = cfg.Coverage
}

var peerProps = markerProps{Certainty: 0.5}
