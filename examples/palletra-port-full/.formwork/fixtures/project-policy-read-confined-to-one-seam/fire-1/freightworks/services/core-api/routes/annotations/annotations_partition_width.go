//go:build ignore

package annotations

// partitionWidthPolicyRead is an OPEN-CODED project-policy read outside the seam: one
// SQL literal names palletra.projects with BOTH policy columns (country +
// facility_type). The mode is even split off into a second constant — the
// concat-split bypass that killed the predecessor — but the columns + table are
// still one literal, so it is still caught (#5169).
const partitionWidthPolicyRead = `SELECT country, facility_type::text FROM palletra.projects WHERE id = $1` // want: project-policy-read-confined-to-one-seam

const partitionWidthPolicyLock = ` FOR UPDATE`
