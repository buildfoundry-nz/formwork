// feature_membership owns its OWN account-scoped mutation and READS the shared
// business-profile via GET — a shared read is legitimate, not duplication.
class ProfileRepository {
  ProfileRepository(this._dio);
  final Dio _dio;

  Future<Profile> loadProfile() {
    return WireDio.get(_dio, '/api/account/business-profile');
  }

  Future<void> upsertContact(Contact c) {
    return WireDio.put(_dio, '/api/account/contact', body: c);
  }
}
