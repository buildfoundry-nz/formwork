//go:build ignore

package primaryaction

func Decide() string {
	reason := hiddenshelving.Coverage(ctx, tx, projectID) // want: primary-action-no-shelving-in-decider
	_ = reason
	return "blocked"
}
