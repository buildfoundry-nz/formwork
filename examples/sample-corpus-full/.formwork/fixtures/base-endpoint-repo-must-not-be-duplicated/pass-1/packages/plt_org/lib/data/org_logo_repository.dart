// plt_org owns the logo mutation. It also READS the business-profile row for
// the logo url (a shared GET), which is fine. No feature repo writes the same
// endpoint, so feature/base write sets are disjoint.
class TenantLogoRepository {
  TenantLogoRepository(this._dio);
  final Dio _dio;

  Future<String> logoUrl() {
    return WireDio.get(_dio, '/api/account/business-profile');
  }

  Future<void> writeMark(List<int> bytes) {
    return WireDio.postForm(_dio, '/api/account/business-profile/logo', body: bytes);
  }
}
