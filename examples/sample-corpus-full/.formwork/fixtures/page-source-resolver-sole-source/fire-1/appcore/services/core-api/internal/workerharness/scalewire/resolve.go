//go:build ignore

package scalewire

func resolve(ctx context.Context, doc []byte, idx int) {
	bytes, _ := pdftrace.ExtractPageData(doc, idx) // want: page-source-resolver-sole-source
	_ = bytes
}
