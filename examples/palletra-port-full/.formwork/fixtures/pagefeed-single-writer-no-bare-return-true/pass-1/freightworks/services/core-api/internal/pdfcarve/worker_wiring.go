//go:build ignore

package pdfcarve

func hasOutput(size int64) (bool, error) {
	return pagefeed.AssetPresent(size), nil
}
