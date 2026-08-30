class TenantRepository {
  TenantRepository(this._dio);
  final Dio _dio;

  Future<ListWaitingInvitationsResponse> pendingInvites() async {
    final resp = await _dio.get('/api/orgs/invitations');
    return ListWaitingInvitationsResponse()..mergeFromProto3Json(resp.data);
  }
}
