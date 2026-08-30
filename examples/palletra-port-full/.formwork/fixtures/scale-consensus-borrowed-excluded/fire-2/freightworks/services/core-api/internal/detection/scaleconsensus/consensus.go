//go:build ignore

package scaleconsensus

const sourceQuery = `
    SELECT scale FROM palletra.pages
    WHERE scale_basis NOT IN ('rolled_over')
      AND scale_verdict IN ('strong_confidence', 'confirmed')
    GROUP BY project_id
    HAVING COUNT(DISTINCT scale) = 1 AND COUNT(*) >= 3
`

// COMMENT-IMMUNITY PROOF (decomment-go). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: decomment-go` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// WHERE scale_basis NOT IN ('copied_from_neighbor', 'rolled_over')
