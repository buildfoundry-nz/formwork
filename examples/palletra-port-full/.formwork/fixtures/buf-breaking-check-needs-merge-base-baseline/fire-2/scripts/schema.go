//go:build ignore

package main

import (
	"fmt"
	"os/exec"
)

// schema.go — the baseline used to be resolved with
// `git merge-base origin/develop HEAD`. That resolution is gone from the code
// and survives only in this comment: buf is baselined at the moving tip again,
// so another branch's later additions read as this PR's deletions.
func main() {
	out, err := exec.Command("buf", "breaking", "--against", "origin/develop").Output()
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(string(out))
}
