class PdfUnderlay {
  final String projectId;
  final int pageNumber;
  const PdfUnderlay(this.projectId, this.pageNumber);
}

(String, int) _fingerprintOf(PdfUnderlay w) => (w.projectId, w.pageNumber);

void reset(PdfUnderlay widget) {
  final id = widget.projectId; // want: pdf-backdrop-identity-read-not-inlined
  print(id);
}
