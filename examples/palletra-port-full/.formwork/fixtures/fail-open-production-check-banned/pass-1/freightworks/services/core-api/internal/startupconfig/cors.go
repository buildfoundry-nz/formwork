//go:build ignore

package startupconfig

func permitLocalhost(e envSource) bool {
	return e.IsDevEnvironment()
}
