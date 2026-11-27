//go:build ignore

package detection

const pageRowQuery = "SELECT pg.status FROM sample.pages pg WHERE pg.id = $1" // want: pages-status-refs-and-enum-cast-banned
