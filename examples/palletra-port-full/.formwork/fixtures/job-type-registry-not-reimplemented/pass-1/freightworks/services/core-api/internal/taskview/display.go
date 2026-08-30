//go:build ignore

package taskview

func labels() []string {
	return projectterms.JobType.Options()
}
