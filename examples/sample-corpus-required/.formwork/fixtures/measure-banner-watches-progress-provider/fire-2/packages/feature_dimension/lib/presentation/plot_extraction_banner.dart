// Progress source removed: the build used to ref.watch(measureParseProgressProvider),
// which now survives only in this comment, so the banner has no live progress
// to show.
class MeasureParseBanner extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    return const Text('Processing');
  }
}
