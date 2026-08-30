import 'package:flutter/material.dart';
import 'package:feature_dimension/presentation/plot_body.dart'; // want: dart-base-packages-forbid-feature-imports

// A base package (plt_widgets) reaching UP into a feature — inverts the arrow.
class PlotChip extends StatelessWidget {
  const PlotChip({super.key});

  @override
  Widget build(BuildContext context) {
    return const PlotBody();
  }
}
