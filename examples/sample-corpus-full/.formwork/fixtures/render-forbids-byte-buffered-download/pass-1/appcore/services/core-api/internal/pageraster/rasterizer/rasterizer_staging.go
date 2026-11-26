//go:build ignore

package rasterizer

// Streams the source PDF to a temp file via the StreamTo seam.
func (r *Rasteriser) stage(ctx context.Context, key string, tmp *os.File) error {
	return r.gcs.StreamTo(ctx, key, tmp)
}
