//go:build ignore

package pagestore

const q = "INSERT INTO sample.pages (id, status, org_id) VALUES ($1, $2, $3)" // want: pages-insert-status-column-banned
