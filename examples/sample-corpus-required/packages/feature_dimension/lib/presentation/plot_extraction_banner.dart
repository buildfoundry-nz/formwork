// Illustrative sample of the extraction banner so the ported rule's scope
// matches a file in this example tree. It watches the live progress source.
class MeasureParseBanner extends ConsumerWidget {
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final progress = ref.watch(measureParseProgressProvider);
    return Text('Processing ${progress.pagesDone} pages');
  }
}
