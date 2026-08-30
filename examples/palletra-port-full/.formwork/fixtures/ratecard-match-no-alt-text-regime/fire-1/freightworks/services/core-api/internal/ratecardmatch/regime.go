//go:build ignore

package ratecardmatch

func compare(row map[string]string) string {
	return row["canonical_key"] // want: ratecard-match-no-alt-text-regime
}
