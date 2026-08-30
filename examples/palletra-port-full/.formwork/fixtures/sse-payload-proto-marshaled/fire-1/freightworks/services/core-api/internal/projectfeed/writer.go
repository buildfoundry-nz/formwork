//go:build ignore

package projectfeed

func emit() {
	v := structpb.NewStringValue("page-123") // want: sse-payload-proto-marshaled
	_ = v
}
