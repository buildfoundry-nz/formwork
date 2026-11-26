//go:build ignore

package skus

func promote(ctx context.Context, mat Sku) {
	m, _ := ratecardmatch.Match(ctx, mat) // want: single-writer-effective-sku-match
	_ = m
}
