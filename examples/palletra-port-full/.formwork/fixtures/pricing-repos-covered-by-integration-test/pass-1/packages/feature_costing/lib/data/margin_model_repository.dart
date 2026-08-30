// MarginModelRepository owns the margin-scenario write paths against the real
// core-api. margin_scenario_test names it directly and asserts the sensitivity
// decode round-trips, so its protojson contract is exercised live.
class MarginModelRepository {
  MarginModelRepository(this._dio);
  final WireDio _dio;

  Future<void> deriveSensitivity(MarginSensitivityRequest req) async {
    await _dio.post('/api/pricing/margin-scenario/sensitivity', req);
  }
}
