import 'package:flutter/widgets.dart';

// Allowlisted: percent display fixed at 1 dp, not a measured quantity.
// except.paths carves this file out.
class MarginOverviewCard extends StatelessWidget {
  const MarginOverviewCard(this.percent, {super.key});

  final double percent;

  @override
  Widget build(BuildContext context) {
    return Text('${percent.toStringAsFixed(1)}%');
  }
}
