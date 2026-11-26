//go:build ignore

package apierr

// The definition site is the ONE place the wire strings are written down, and
// scope excludes it: banning them here would ban the enum itself.
const (
	RevisionConflict   Code = "revision_conflict"
	CapabilityDisabled Code = "capability_disabled"
	ResourceLocked     Code = "resource_locked"
)

// Wire renders a typed code for the wire.
func Wire(c Code) string { return string(c) }
