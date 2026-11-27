//go:build ignore

package orgs

// Handler serves org routes.
type Handler struct{ db DB }

func (h *Handler) BootstrapOrg(ctx context.Context) error {
	return h.db.AsSuperuser(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "INSERT INTO platform.orgs (id) VALUES ($1)", newID())
		return err
	})
}
