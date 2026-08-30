//go:build ignore

// The guard is gone and only a comment names it. This is the defect the cure
// describes — a run with no arguments indexes os.Args[1] and panics, or with
// mode: exists deploys the empty tag — wearing the shape of the fix.
package main

import (
	"fmt"
	"os"
	"os/exec"
)

func main() {
	// usage: deploy <image-tag>
	tag := os.Args[1]

	fmt.Println("deploying", tag)
	run("kubectl", "set", "image", "deployment/api", "api=registry.example.com/api:"+tag)
	run("kubectl", "rollout", "status", "deployment/api", "--timeout=120s")
}

func run(name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		os.Exit(1)
	}
}
