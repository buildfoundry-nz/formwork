//go:build ignore

package drawinghash

import (
	"strconv"
	"strings"
)

// encodeVectorLiteral builds a pgvector text literal from a float slice — a second
// copy of the codec that must live only in internal/pgvector.
func encodeVectorLiteral(vec []float32) string {
	var b strings.Builder
	b.WriteByte('[') // want: pgvector-codec-single-owner
	for i, v := range vec {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(v), 'f', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}
