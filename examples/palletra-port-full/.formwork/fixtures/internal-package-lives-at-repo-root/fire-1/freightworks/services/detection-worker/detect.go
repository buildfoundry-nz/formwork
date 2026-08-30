//go:build ignore

package detectworker

import (
	metrics "github.com/palletra/freightworks/services/core-api/internal/metrics" // want: internal-package-lives-at-repo-root
)

var _ = metrics.Foo
