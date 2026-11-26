//go:build ignore

package diagnostics

type inferencePipelineResponse struct {
	VisionkitApiKeyPresent bool
	Workspace              string
	Bucket                 string
}

func handle(cfg debugConfig) inferencePipelineResponse {
	return inferencePipelineResponse{
		Workspace: cfg.Workspace,
		Bucket:    cfg.Bucket,
	}
}
