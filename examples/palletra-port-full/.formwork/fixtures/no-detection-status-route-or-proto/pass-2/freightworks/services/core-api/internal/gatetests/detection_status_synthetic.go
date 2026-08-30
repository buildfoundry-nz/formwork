//go:build ignore

package gatetests

// Synthetic fixture: intentionally plants the removed surface to exercise the
// gate. This lives under internal/gatetests and is excluded from the ban.
const seededRoute = "/api/projects/{projectID}/detection-status"
const seededProto = "PipelineStatusResponse"
