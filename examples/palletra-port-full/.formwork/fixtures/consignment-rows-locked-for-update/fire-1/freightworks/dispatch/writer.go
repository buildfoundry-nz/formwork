package dispatch

const lockParent = `
	SELECT c.id, c.status
	  FROM ops.consignments AS c
	  JOIN ops.consignment_lines AS l ON l.consignment_id = c.id
	 WHERE c.id = $1
	   FOR UPDATE OF c
`
