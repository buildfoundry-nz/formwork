class IngestRepository {
  // Single-pass: one call, no client-side paging constant.
  Future<void> classifyRecommendations() async {
    await _api.classifyRecommend();
  }
}
