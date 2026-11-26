import 'package:flutter_riverpod/flutter_riverpod.dart';

final sheetSlotProvider = Provider.family<int, String>((ref, pageId) {
  final annotations = ref.watch(projectCalloutsProvider(projectId)); // want: dart-project-annotations-watch-requires-select
  return annotations.value.markerTalliesForPage(pageId).length;
});
