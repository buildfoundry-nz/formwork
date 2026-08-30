import 'package:flutter_riverpod/flutter_riverpod.dart';

final sheetSlotProvider = Provider.family<int, String>((ref, pageId) {
  final metrics = ref.watch(projectCalloutsProvider(projectId).select(
    (a) => a.value.markerTalliesForPage(pageId) ?? const <MarkerTally>[]));
  return metrics.length;
});
