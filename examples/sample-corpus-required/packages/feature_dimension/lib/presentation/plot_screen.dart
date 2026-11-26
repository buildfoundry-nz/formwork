// Illustrative sample of the measure screen so the ported rule's scope matches
// a file in this example tree. It mounts the extraction banner.
class PlotScreen extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    return Column(children: const [MeasureParseBanner()]);
  }
}
