//go:build ignore

package workflow

// The typed builders. The raw names projectfeed.WriteEvent( and
// projectfeed.WriteSignalParams{ appear here only in this comment, which
// decomment-go blanks before the pattern runs.
func emitStepDone(feed *projectfeed.Feed, stepRef string) {
	projectfeed.WriteStepDone(feed, stepRef)
}

func emitStepSignal(feed *projectfeed.Feed, stepRef string) {
	projectfeed.WriteStepSignal(feed, stepRef)
}
