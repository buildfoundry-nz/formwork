Future<PdfLease> pdfPageLease(Ref ref, String projectId, int pageNumber) async {
  ref.pinInProjectCache(projectId); // want: flutter-pdf-page-handle-retain-not-discarded
  return acquireHandle(projectId, pageNumber);
}
