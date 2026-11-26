//go:build ignore

package spancheck

const downgradeSQL = `
    UPDATE sample.pages
    SET scale_verdict = 'requires_review', scale_certainty = 0
    WHERE id = $1
`
