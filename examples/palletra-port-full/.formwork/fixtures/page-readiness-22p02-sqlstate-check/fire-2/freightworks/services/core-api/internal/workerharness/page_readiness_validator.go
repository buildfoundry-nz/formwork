//go:build ignore

package workerharness

// Minimal checker missing the fail-loud 22P02 wiring.
func countPreparedPages() error {
	return nil
}

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// if errors.As(runErr, &pgErr) && pgErr.Code == "22P02" {
