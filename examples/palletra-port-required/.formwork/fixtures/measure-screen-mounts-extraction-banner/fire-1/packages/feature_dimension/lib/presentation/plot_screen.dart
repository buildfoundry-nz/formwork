// Banner mount removed: a first-slot-ready user is stranded with no indicator.
class PlotScreen extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    return const Column(children: [PlotCanvas()]);
  }
}
