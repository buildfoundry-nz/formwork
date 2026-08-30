class TenantRepository {
  TenantRepository(this._dio);
  final Dio _dio;

  Future<ListWaitingInvitationsResponse> pendingInvites({String pageToken = ''}) async {
    final resp = await _dio.get('/api/orgs/invitations', queryParameters: {'page_token': pageToken});
    return ListWaitingInvitationsResponse()..mergeFromProto3Json(resp.data);
  }
}
