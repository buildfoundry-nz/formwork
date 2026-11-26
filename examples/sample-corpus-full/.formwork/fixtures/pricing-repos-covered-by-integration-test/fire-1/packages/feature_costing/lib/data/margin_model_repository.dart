// MarginModelRepository owns the margin-scenario write paths (create / update
// sensitivity) against the real core-api. No pricing integration test references
// it — pricing_backbone_test only names CostingRepository — so its protojson
// round-trip is never exercised against the live backend.
class MarginModelRepository {
  MarginModelRepository(this._dio);
  final WireDio _dio;

  Future<void> deriveSensitivity(MarginSensitivityRequest req) async {
    await _dio.post('/api/pricing/margin-scenario/sensitivity', req);
  }
}
