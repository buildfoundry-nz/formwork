//go:build ignore

package chunking

// The single place classify-chunks object keys are minted.
const segmentObjectPrefix = "classify-chunks/"

func objectKey(jobID string, n int) string {
	return segmentObjectPrefix + jobID + "/" + itoa(n) + ".pdf"
}
