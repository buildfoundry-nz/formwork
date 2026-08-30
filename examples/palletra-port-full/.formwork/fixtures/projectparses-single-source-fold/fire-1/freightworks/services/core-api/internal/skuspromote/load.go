//go:build ignore

package skuspromote

const loadClusters = `
	SELECT descriptor FROM extracted_skus
	GROUP BY descriptor, lower(trim(COALESCE(unit, '')))` // want: projectparses-single-source-fold
