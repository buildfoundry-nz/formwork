class PlotApply {
  void applyChange(WidgetRef ref, String projectId, int pageId, Object delta) {
    ref.read(workflowRailSyncProvider(projectId, pageId).notifier).fold(delta); // want: flutter-rail-fold-only-via-ticket-api
  }
}
