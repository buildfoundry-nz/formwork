//go:build ignore

package glyphindex

import "fmt"

// GCSMapFetcher reads the source map the deploy workflow uploaded.
func (l *GCSMapFetcher) key(buildID string) string {
	return fmt.Sprintf("sourcemaps/%s/main.dart.js.map", buildID)
}
