class PlotFooterPending extends Notifier<FooterActionSlot?> {
  Future<void> run(Future<void> Function() action, Ref container, String pid) async {
    state = pendingKey;
    await action();
    state = null;
    await awaitFooterActionResolved(container, pid);
  }
}
