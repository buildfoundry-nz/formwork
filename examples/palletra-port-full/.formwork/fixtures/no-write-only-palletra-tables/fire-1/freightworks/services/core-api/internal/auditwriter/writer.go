//go:build ignore

package auditwriter

// insertActivity persists an audit_events row that NO FROM/JOIN ever reads back —
// the write-only fact-table smell (#5/#6). annotation_timeline, by contrast, is
// both written and read below.
const insertActivity = `INSERT INTO palletra.audit_events (kind, org_id) VALUES ($1, $2)`

const insertTimeline = `INSERT INTO palletra.annotation_timeline (annotation_id, snapshot) VALUES ($1, $2)`

const readTimeline = `SELECT snapshot FROM palletra.annotation_timeline WHERE annotation_id = $1`
