import 'package:flutter_riverpod/flutter_riverpod.dart';

void boot(WidgetRef ref) {
  ref.read(clientTelemetryReporterProvider.future); // want: flutter-reporter-start-via-guarded-helper
}
