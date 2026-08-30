//go:build ignore

package events

import "google.golang.org/protobuf/types/known/structpb"

// Events carry routing ids only. A payload keyed "impactedPage": {...} would
// breach the 8KB WriteEvent cap, so it is deliberately absent here.
func composeAnnotationEventPayload() *structpb.Struct {
	fields := map[string]any{
		"annotationId": "a-1",
		"pageId":       "p-1",
		"projectId":    "proj-1",
	}
	s, _ := structpb.NewStruct(fields)
	return s
}
