//go:build ignore

package workflow

func advance(ctx context.Context) (Nav, error) {
	return deriveNextSectionAndPage(ctx) // want: workflow-next-section-walk-sole-engine
}
