class PlotScreen extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    return Column(children: const [MeasureParseBanner(), PlotCanvas()]);
  }
}
