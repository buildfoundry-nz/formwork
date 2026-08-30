import 'package:flutter_riverpod/flutter_riverpod.dart';

// The footer-flip seam: the ONLY site allowed to read the optimistic notifier
// (carved out via except.paths). It always clears on throw.
void publishFooterToggle(WidgetRef ref, String projectId, Object action) {
  ref.read(plotOptimisticPrimaryActionProvider(projectId).notifier).show(action);
}

void resetFooterFlip(WidgetRef ref, String projectId) {
  ref.read(plotOptimisticPrimaryActionProvider(projectId).notifier).clear();
}

Future<void> runWithFooterToggle(
  WidgetRef ref,
  String projectId,
  Object action,
  Future<void> Function() body,
) async {
  publishFooterToggle(ref, projectId, action);
  try {
    await body();
  } catch (_) {
    resetFooterFlip(ref, projectId);
    rethrow;
  }
}
