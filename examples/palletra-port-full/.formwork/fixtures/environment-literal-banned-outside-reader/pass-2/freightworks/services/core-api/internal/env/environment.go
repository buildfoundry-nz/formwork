//go:build ignore

package env

import "os"

// environment.go is the ONE sanctioned reader of the ENVIRONMENT variable, so
// it is excluded from the gate — the literal here must NOT fire.
func IsDevEnvironment() bool {
	return os.Getenv("ENVIRONMENT") == "local"
}
