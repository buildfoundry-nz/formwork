package src

// One trigger line in its own file passes the census; only locks.go offends.
func d() { db.lockThing() }
