//go:build ignore

package pdfcarve

func hasOutput(size int64) (bool, error) {
	if size > 0 {
		return true, nil
	}
	return false, nil
}
