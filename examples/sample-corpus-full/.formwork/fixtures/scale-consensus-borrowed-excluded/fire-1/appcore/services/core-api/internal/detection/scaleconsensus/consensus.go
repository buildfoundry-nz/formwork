//go:build ignore

package scaleconsensus

const sourceQuery = `
    SELECT scale FROM sample.pages
    WHERE scale_basis NOT IN ('rolled_over')
      AND scale_verdict IN ('strong_confidence', 'confirmed')
    GROUP BY project_id
    HAVING COUNT(DISTINCT scale) = 1 AND COUNT(*) >= 3
`
