//go:build ignore

package diagnostics

import "os"

type inferencePipelineResponse struct {
	VisionkitApiKey string // want: diagnostics-handler-no-secret-leak
	Workspace       string
}

func handle(dbErr error) inferencePipelineResponse {
	_ = os.Getenv("VISIONKIT_API_KEY") // want: diagnostics-handler-no-secret-leak
	return inferencePipelineResponse{
		Workspace: dbErr.Error(), // want: diagnostics-handler-no-secret-leak
	}
}
