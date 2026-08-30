class PlotApply {
  Future<void> applyChange(
    WidgetRef ref,
    String projectId,
    int pageId,
    Future<WriteResponse> Function() run,
  ) async {
    final railSync =
        ref.read(workflowRailSyncProvider(projectId, pageId).notifier);
    final ticket = railSync.openMutation();
    try {
      final resp = await run();
      railSync.finishMutation(ticket, pageId: pageId, delta: resp.completionDiff);
    } catch (_) {
      railSync.cancelMutation(ticket);
      rethrow;
    }
  }
}
