class MeasureParseBanner extends ConsumerWidget {
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final progress = ref.watch(measureParseProgressProvider);
    return Text('Processing ${progress.pagesDone} pages');
  }
}
