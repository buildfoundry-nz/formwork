package dispatch

// Locks the parent at SHARE strength, and the exclusive lock is taken on the
// LINE alias — a different relation, which this rule must not fire on.
const lockParent = `
	SELECT c.id, c.status
	  FROM ops.consignments AS c
	  JOIN ops.consignment_lines AS l ON l.consignment_id = c.id
	 WHERE c.id = $1
	   FOR SHARE OF c
	   FOR UPDATE OF l
`

// A different schema entirely: the analytics view is read-only and locking it
// at share strength is expected. `schema: ^ops$` must exclude this.
const analyticsPeek = `
	SELECT a.id FROM analytics.consignments AS a WHERE a.id = $1 FOR UPDATE OF a
`
