// Package src is the witness corpus: 14 production call sites of the tenancy
// seam, plus one in prose. It is scanned by the census through the engine's own
// scan.Walk, and it is COMPILED — every commit in a PR range is `go test -c`'d
// (scripts/check-commits-build.sh) and every .go file is gofmt-checked, and a
// fixture is not exempt from either.
//
// The seam is a struct FIELD of func type rather than a method, deliberately:
// a method declaration would itself read `WithTenantOrg(` and make the corpus
// 15 witnesses, silently moving the number this fixture exists to pin. As a
// field the declaration reads `WithTenantOrg func(`, which the arm's
// `WithTenantOrg\(` pattern does not match.
package src

var (
	db struct {
		WithTenantOrg func(ctx, p, o, f int) error
	}
	ctx, p, o, f int
)

func w0()  { _ = db.WithTenantOrg(ctx, p, o, f) }
func w1()  { _ = db.WithTenantOrg(ctx, p, o, f) }
func w2()  { _ = db.WithTenantOrg(ctx, p, o, f) }
func w3()  { _ = db.WithTenantOrg(ctx, p, o, f) }
func w4()  { _ = db.WithTenantOrg(ctx, p, o, f) }
func w5()  { _ = db.WithTenantOrg(ctx, p, o, f) }
func w6()  { _ = db.WithTenantOrg(ctx, p, o, f) }
func w7()  { _ = db.WithTenantOrg(ctx, p, o, f) }
func w8()  { _ = db.WithTenantOrg(ctx, p, o, f) }
func w9()  { _ = db.WithTenantOrg(ctx, p, o, f) }
func w10() { _ = db.WithTenantOrg(ctx, p, o, f) }
func w11() { _ = db.WithTenantOrg(ctx, p, o, f) }
func w12() { _ = db.WithTenantOrg(ctx, p, o, f) }
func w13() { _ = db.WithTenantOrg(ctx, p, o, f) }

// db.WithTenantOrg(ctx, p, o, f) — prose, not a witness
