//go:build ignore

package coreapi

// The newDetectionWorker helper has been removed — the extraction is undone.
func MountApp(d Deps) {
	derivedIdentificationAutoRun := detection.NewDetectionWorkerHandler(IdentificationRunnerConfig{DB: d.DB})
	_ = derivedIdentificationAutoRun
}
