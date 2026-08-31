// Package src is the fire corpus: two lock-call lines in ONE file.
// The seam is a struct FIELD of func type (the arm-census idiom) so the
// declaration line itself does not match the trigger pattern and the pinned
// count is exactly two.
package src

var db struct {
	lockThing   func()
	unlockThing func()
}

func a() { db.lockThing() }
func b() { db.lockThing() }
func c() { db.unlockThing() }
