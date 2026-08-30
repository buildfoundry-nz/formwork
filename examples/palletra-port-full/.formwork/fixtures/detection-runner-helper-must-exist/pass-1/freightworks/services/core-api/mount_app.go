//go:build ignore

package coreapi

// The extraction is intact: the helper exists.
func newDetectionWorker(d Deps) *detection.IdentificationRunnerHandler {
	return detection.NewDetectionWorkerHandler(IdentificationRunnerConfig{DB: d.DB})
}
