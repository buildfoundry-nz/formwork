@riverpod
class PendingInvites extends _$PendingInvites {
  Future<void> load() async {
    final page = await ref.read(tenantRepositoryProvider).pendingInvites();
    state = page.invitations;
  }
}
