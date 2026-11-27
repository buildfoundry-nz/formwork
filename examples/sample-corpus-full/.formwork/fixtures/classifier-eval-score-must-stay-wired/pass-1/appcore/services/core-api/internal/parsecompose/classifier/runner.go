//go:build ignore

package classifier

func Run(pairs []Pair) {
	report := eval.Score(pairs)
	_ = report
}
