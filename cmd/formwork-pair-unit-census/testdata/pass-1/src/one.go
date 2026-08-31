// Package src is the pass corpus: one trigger line per Go file.
package src

var db struct {
	lockThing   func()
	unlockThing func()
}

func a() { db.lockThing() }
func c() { db.unlockThing() }
