//go:build ignore

package events

import "google.golang.org/protobuf/types/known/structpb"

func composeAnnotationEventPayload(ap any) *structpb.Struct {
	fields := map[string]any{
		"annotationId": "a-1",
		"pageId":       "p-1",
		"impactedPage": ap, // want: no-affectedpage-payload-in-project-events
	}
	s, _ := structpb.NewStruct(fields)
	return s
}
