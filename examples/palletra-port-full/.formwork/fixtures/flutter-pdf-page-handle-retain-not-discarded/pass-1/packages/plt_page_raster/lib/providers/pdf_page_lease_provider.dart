Future<PdfLease> pdfPageLease(Ref ref, String projectId, int pageNumber) async {
  final release = ref.pinInProjectCache(projectId);
  final lru = ref.watch(pdfPageLeaseLruProvider);
  lru.retain(projectId, pageNumber, release);
  return acquireHandle(projectId, pageNumber);
}
