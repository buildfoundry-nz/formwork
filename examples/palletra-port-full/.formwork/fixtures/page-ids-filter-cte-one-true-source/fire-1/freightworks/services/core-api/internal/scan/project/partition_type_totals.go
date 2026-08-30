//go:build ignore

package project

func query() string {
	return `WITH page_scope_filter AS (SELECT unnest($2::text[]) AS pid)` // want: page-ids-filter-cte-one-true-source
}
