//go:build ignore

package scaleconsensus

const sourceQuery = `
    SELECT scale FROM palletra.pages
    WHERE scale_basis NOT IN ('copied_from_neighbor')
      AND scale_verdict IN ('strong_confidence', 'confirmed')
    GROUP BY project_id
    HAVING COUNT(DISTINCT scale) = 1 AND COUNT(*) >= 3
`
