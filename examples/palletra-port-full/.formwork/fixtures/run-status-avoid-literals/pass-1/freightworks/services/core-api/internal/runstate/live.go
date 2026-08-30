//go:build ignore

package runstate

// The canonical definition itself names the literal set; it is the one place
// allowed to, so this excluded path must NOT trip the rule even though it
// carries status IN ('pending','running').
func Live() []string { return []string{"pending", "running"} }

const doc = `status IN ('pending','running')`
