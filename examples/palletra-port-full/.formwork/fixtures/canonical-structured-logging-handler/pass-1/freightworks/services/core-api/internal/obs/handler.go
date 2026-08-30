//go:build ignore

package obs

import "github.com/palletra/freightworks/internal/logging"

// setup routes all logging through the canonical internal/logging package,
// whose Init installs the Cloud-Logging-shaped handler (level->severity,
// msg->message). No raw slog handler is constructed here.
func setup() {
	logging.Init("core-api")
}
