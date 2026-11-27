//go:build ignore

package scalewire

import "context"

// stage downloads the whole source PDF — the #4870/#2765 whole-doc-staging OOM.
func stage(ctx context.Context, t task) ([]byte, error) {
	return download(ctx, t.OriginPDFKey) // want: scale-detect-no-source-pdf-download
}
