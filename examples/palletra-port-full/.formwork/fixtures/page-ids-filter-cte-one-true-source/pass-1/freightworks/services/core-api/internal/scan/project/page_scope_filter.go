//go:build ignore

package project

const PageScopeFilterCTE = `WITH page_scope_filter AS (SELECT unnest($2::text[]) AS pid)`
