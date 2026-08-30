// Banner mount removed: the Column used to carry MeasureParseBanner(), which
// now survives only in this comment, so a first-slot-ready user is stranded
// with no indicator.
class PlotScreen extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    return const Column(children: [PlotCanvas()]);
  }
}
