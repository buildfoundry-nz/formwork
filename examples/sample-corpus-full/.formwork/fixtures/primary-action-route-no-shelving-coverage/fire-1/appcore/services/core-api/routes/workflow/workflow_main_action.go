//go:build ignore

package workflow

func MainAction() {
	cov := hiddenshelving.Coverage(ctx, tx, projectID) // want: primary-action-route-no-shelving-coverage
	_ = cov
}
