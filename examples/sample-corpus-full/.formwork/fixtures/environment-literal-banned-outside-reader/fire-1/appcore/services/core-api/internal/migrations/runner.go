//go:build ignore

package migrations

import "os"

func autoResync() bool {
	env := os.Getenv("ENVIRONMENT") // want: environment-literal-banned-outside-reader
	return env == "local"
}
