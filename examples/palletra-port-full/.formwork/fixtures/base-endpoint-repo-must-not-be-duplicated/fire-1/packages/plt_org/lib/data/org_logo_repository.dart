// plt_org already OWNS the business-profile logo mutation. feature_membership
// depends on plt_org yet re-wraps the same POST+DELETE — the shadow repo.
class TenantLogoRepository {
  TenantLogoRepository(this._dio);
  final Dio _dio;

  Future<void> writeMark(List<int> bytes) {
    return WireDio.postForm(_dio, '/api/account/business-profile/logo', body: bytes);
  }

  Future<void> clearLogo() {
    return WireDio.delete(_dio, '/api/account/business-profile/logo');
  }
}
