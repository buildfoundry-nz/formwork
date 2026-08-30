//go:build ignore

package startupconfig

func allowOrigin(e envSource) bool {
	return e.IsDevEnvironment()
}
