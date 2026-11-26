import 'package:flutter/widgets.dart';

// Only an InteractiveViewer with a single-widget child — no Stack. Allowed.
class PlotCanvasViewer extends StatelessWidget {
  const PlotCanvasViewer({super.key});

  @override
  Widget build(BuildContext context) {
    return InteractiveViewer(
      minScale: 0.5,
      maxScale: 4,
      child: const SizedBox(width: 800, height: 600),
    );
  }
}
