//go:build ignore

package projectfeed

// The owning package holds the raw writer and is excluded from scope: the typed
// builders below are the only sanctioned callers of it.
func WriteEvent(f *Feed, name string, body *structpb.Struct) { f.write(name, body) }

// WriteSignalParams is the raw parameter struct the builders fill in.
type WriteSignalParams struct {
	Name string
	Body *structpb.Struct
}

// WriteStepDone is one of the typed builders callers outside this package use.
func WriteStepDone(f *Feed, stepRef string) { WriteEvent(f, "step.done", stepDone(stepRef)) }
