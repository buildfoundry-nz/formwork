class PlotRailBulkActions extends ConsumerWidget {
  const PlotRailBulkActions({super.key, required this.busy});
  final FooterActionSlot? busy;
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return PendingButton(
      busy: busy == batchActionKey(action),
      onPressed: () {},
      child: const Text('Approve All'),
    );
  }
}
