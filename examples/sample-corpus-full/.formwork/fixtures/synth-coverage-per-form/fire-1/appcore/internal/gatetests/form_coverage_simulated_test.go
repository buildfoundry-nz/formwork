//go:build ignore

package gatetests

// This synth declares the forms each class-locking gate exercises.
//
// FORM(bare-pool): struct-field
// FORM(bare-pool): pgxpool-new
// PASSFORM(bare-pool): tenant-scoped-pool
//
// FORM(blind-write): raw-string
// FORM(blind-write): dquote-string
//
// FORM(pdftrace): direct-open
// FORM(pdftrace): cached-open
// PASSFORM(pdftrace): admission-gated
//
// SINGLEFORM(no-drop): a destructive DROP TABLE has exactly one form to lock

func TestFormCoverageMarkers(t *testing.T) {}
