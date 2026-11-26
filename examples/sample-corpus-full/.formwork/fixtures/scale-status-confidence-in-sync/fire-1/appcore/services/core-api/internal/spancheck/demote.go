//go:build ignore

package spancheck

const downgradeSQL = `
    UPDATE sample.pages -- want: scale-status-confidence-in-sync
    SET scale_verdict = 'requires_review'
    WHERE id = $1
`
