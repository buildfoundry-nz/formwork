// Package repoproof holds gates over this repository's OWN tooling — the
// Makefile's verdict handling, the clone harness's refusals, the installed
// git hooks — as Go tests rather than shell scripts.
//
// WHY IT EXISTS AS GO. This project's entire claim is that a fleet of shell
// gate scripts should be one binary with tests. A repository selling that
// while gating itself with 369-line bash proofs is arguing against its own
// product in the first place a reader looks, and the public tree is exactly
// where a reader looks first.
//
// These are not rules and could not be. A formwork rule reads tracked file
// CONTENT; what these assert is process behaviour — an exit status
// propagating through a pipeline, a hook firing on a real commit, a harness
// refusing an empty manifest. That is what a test is for, and the repo
// already runs the CLI end-to-end this way in internal/cli.
//
// The package deliberately contains no production code. It exists so
// `go test ./...` reaches these, and so nothing in cmd/ or the engine can
// import them by accident.
package repoproof
