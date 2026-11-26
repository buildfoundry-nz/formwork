//go:build ignore

package spancheck

const selectStaleSQL = `
    SELECT id FROM sample.pages
    WHERE scale_verdict = 'requires_review'
`
