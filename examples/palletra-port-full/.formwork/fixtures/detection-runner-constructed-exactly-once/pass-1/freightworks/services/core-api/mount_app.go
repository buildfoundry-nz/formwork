//go:build ignore

package coreapi

// The single construction site: both runners route through this helper.
func newDetectionWorker(d Deps) *detection.IdentificationRunnerHandler {
	// A commented-out earlier construction must not inflate the count:
	// return detection.NewDetectionWorkerHandler(IdentificationRunnerConfig{DB: d.DB, Legacy: true})
	return detection.NewDetectionWorkerHandler(IdentificationRunnerConfig{DB: d.DB})
}

func MountApp(d Deps) {
	derivedIdentificationAutoRun := newDetectionWorker(d)
	wave11DetectionRunner := newDetectionWorker(d)
	_ = derivedIdentificationAutoRun
	_ = wave11DetectionRunner
}
