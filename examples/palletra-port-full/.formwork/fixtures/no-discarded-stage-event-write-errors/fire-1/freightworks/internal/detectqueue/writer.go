//go:build ignore

package detectqueue

func (w *worker) run(ctx context.Context) {
	_ = w.cfg.Events.WriteBegun(ctx) // want: no-discarded-stage-event-write-errors
}
