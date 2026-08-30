//go:build ignore

package detection

const pageRowQuery = "SELECT pg.intake_status FROM palletra.pages pg WHERE pg.id = $1"
