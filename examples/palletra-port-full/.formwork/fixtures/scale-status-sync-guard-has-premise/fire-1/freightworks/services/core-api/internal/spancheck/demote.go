//go:build ignore

package spancheck

const selectStaleSQL = `
    SELECT id FROM palletra.pages
    WHERE scale_verdict = 'requires_review'
`
