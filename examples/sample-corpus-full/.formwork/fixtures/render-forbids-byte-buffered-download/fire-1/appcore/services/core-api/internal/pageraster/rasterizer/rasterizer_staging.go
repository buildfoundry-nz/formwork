//go:build ignore

package rasterizer

// Byte-returning Download pulls the whole source PDF onto the Go heap.
func (r *Rasteriser) stage(ctx context.Context, key string) ([]byte, error) {
	data, err := r.gcs.Download(ctx, key) // want: render-forbids-byte-buffered-download
	return data, err
}
