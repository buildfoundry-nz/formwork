class WorkflowRailSync extends AsyncNotifier<void> {
  void request(Ref ref) => ref.invalidate(plotPrimaryActionProvider);
  void fold(Ref ref) => ref.invalidate(plotPrimaryActionProvider);
}
