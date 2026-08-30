//go:build ignore

package primaryaction

// The footer decider must stay agnostic of hidden_shelving coverage.
func Decide() string {
	return "progress"
}
