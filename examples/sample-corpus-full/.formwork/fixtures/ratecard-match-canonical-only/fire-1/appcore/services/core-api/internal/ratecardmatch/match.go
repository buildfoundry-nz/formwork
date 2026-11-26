//go:build ignore

package ratecardmatch

func shortlistQuery() string {
	return `SELECT id FROM catalog WHERE canonical_key = $1` // want: ratecard-match-canonical-only
}
