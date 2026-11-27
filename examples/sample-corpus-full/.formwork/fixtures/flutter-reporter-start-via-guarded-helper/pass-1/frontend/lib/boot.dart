import 'package:flutter_riverpod/flutter_riverpod.dart';

void boot(WidgetRef ref) {
  startClientEventTracker(ref.read(clientTelemetryReporterProvider.future));
}
