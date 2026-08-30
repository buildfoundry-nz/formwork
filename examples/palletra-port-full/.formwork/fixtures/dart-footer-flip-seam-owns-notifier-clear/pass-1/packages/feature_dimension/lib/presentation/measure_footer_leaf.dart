import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:feature_dimension/providers/footer_session_toggle.dart';

// A compliant leaf: it routes through the seam, never touching the notifier.
class PlotFooterLeaf {
  Future<void> onScaleAccepted(WidgetRef ref, String projectId) {
    return runWithFooterToggle(ref, projectId, 'after_scale', () async {
      await Future<void>.delayed(Duration.zero);
    });
  }
}
