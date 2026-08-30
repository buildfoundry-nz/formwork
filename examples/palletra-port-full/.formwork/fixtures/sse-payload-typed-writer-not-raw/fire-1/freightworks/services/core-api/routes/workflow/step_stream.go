//go:build ignore

package workflow

// Both raw seams, named from outside the owning package: either one hands the
// stream an untyped payload the builders exist to make impossible.
func emitStepDone(feed *projectfeed.Feed, stepRef string) {
	projectfeed.WriteEvent(feed, "step.done", untypedPayload(stepRef)) // want: sse-payload-typed-writer-not-raw
}

func emitStepSignal(feed *projectfeed.Feed, stepRef string) {
	params := projectfeed.WriteSignalParams{ // want: sse-payload-typed-writer-not-raw
		Name: "step.signal",
		Body: untypedPayload(stepRef),
	}
	feed.Signal(params)
}
