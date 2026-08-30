// want: dart-canvas-viewer-stack-shape-not-duplicated
import 'package:flutter/widgets.dart';

class PlotCanvasViewer extends StatelessWidget {
  const PlotCanvasViewer({super.key});

  @override
  Widget build(BuildContext context) {
    return InteractiveViewer(
      minScale: 0.5,
      maxScale: 4,
      child: Stack(
        children: const [
          SizedBox(width: 800, height: 600),
        ],
      ),
    );
  }
}
