import 'package:flutter_riverpod/flutter_riverpod.dart';

// A leaf re-deriving show/clear for itself instead of routing through the seam.
class PlotFooterLeaf {
  void onScaleAccepted(WidgetRef ref, String projectId) {
    ref.read(plotOptimisticPrimaryActionProvider(projectId).notifier).clear(); // want: dart-footer-flip-seam-owns-notifier-clear
  }
}
