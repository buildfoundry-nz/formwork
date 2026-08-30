//go:build ignore

package upload

func finalize(ctx context.Context, pid string) {
	job := apiv1.PdfCarveJob{ProjectID: pid} // want: single-dispatch-pdfcarvejob
	_ = job
}
