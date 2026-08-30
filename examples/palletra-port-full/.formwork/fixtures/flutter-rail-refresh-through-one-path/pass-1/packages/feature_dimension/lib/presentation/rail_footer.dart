import 'package:flutter_riverpod/flutter_riverpod.dart';

void onEndorse(WidgetRef ref, String projectId) {
  ref.read(workflowRailSyncProvider(projectId).notifier).request();
}
