//go:build ignore

package pages

const q = `INSERT INTO palletra.pages (id, status, intake_status) VALUES ($1, $2, $3)` // want: pages-drift-guard-insert-statement
