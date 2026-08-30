//go:build ignore

package coreapi

func MountApp(d Deps) {
	// Recurrence: two inlined construction sites instead of two helper calls.
	derivedIdentificationAutoRun := detection.NewDetectionWorkerHandler(IdentificationRunnerConfig{DB: d.DB})
	wave11DetectionRunner := detection.NewDetectionWorkerHandler(IdentificationRunnerConfig{DB: d.DB})
	_ = derivedIdentificationAutoRun
	_ = wave11DetectionRunner
}
