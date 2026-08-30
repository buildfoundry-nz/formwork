//go:build ignore

package detectqueue

func (w *worker) run(ctx context.Context) error {
	if err := w.cfg.Events.WriteBegun(ctx); err != nil {
		return err
	}
	return nil
}
