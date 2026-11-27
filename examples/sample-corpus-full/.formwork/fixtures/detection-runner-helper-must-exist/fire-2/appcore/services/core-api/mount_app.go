//go:build ignore

package coreapi

// The newDetectionWorker helper has been removed — the extraction is undone.
func MountApp(d Deps) {
	derivedIdentificationAutoRun := detection.NewDetectionWorkerHandler(IdentificationRunnerConfig{DB: d.DB})
	_ = derivedIdentificationAutoRun
}

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// func newDetectionWorker(d Deps) *detection.IdentificationRunnerHandler {
