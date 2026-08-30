@riverpod
class PendingInvites extends _$PendingInvites {
  Future<void> fetchMore() async {
    final page = await ref.read(tenantRepositoryProvider).pendingInvites(pageToken: _cursor);
    _cursor = page.nextPageToken;
    state = [...state, ...page.invitations];
  }

  String _cursor = '';
}
