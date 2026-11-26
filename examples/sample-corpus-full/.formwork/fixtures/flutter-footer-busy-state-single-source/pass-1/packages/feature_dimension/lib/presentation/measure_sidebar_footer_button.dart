class PlotSidebarFooterButton extends ConsumerWidget {
  const PlotSidebarFooterButton({super.key});
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final resolved = chooseFooterSpinner(ref.watch(plotFooterBusy));
    return Column(children: [
      PlotPrimaryActionButton(busy: resolved),
      PlotRailBulkActions(busy: resolved),
    ]);
  }
}
