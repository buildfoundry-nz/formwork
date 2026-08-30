//go:build ignore

// No usage guard: a run with no arguments deploys the empty tag and exits 0.
package main

import (
	"fmt"
	"os"
	"os/exec"
)

func main() {
	var tag string
	if len(os.Args) > 1 {
		tag = os.Args[1]
	}

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
