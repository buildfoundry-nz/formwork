class PlotPrimaryActionButton extends ConsumerWidget {
  const PlotPrimaryActionButton({super.key, required this.busy});
  final FooterActionSlot? busy;
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return FilledButton(
      busy: busy == mainActionKey(action),
      onPressed: () {},
      child: const Text('Approve'),
    );
  }
}
