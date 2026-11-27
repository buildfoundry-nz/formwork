// feature_membership open-codes the business-profile logo MUTATION — a byte
// duplicate of plt_org.TenantLogoRepository (audit #8 shadow repo).
class ProfileRepository {
  ProfileRepository(this._dio);
  final Dio _dio;

  Future<void> uploadMark(List<int> bytes) {
    return WireDio.postForm(_dio, '/api/account/business-profile/logo', body: bytes);
  }

  Future<void> removeEmblem() {
    return WireDio.delete(_dio, '/api/account/business-profile/logo');
  }
}
