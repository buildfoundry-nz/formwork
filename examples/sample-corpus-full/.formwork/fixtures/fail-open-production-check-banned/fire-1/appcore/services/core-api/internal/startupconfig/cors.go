//go:build ignore

package startupconfig

func permitLocalhost(environment string) bool {
	return environment != "production" // want: fail-open-production-check-banned
}
