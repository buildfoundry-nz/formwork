//go:build ignore

package classifier

// The runner used to call eval.Score(pairs), but that is only prose now — no
// live consumer remains, so the §8 regression gate is unwired.
func Run() {
	_ = "no scoring wired"
}
