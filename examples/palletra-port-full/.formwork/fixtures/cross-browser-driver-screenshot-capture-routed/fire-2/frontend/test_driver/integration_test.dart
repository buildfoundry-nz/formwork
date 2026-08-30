import 'package:integration_test/integration_test_driver_extended.dart';

Future<void> main() async {
  // The callback used to return handleCrossBrowserCapture(name, bytes, args).
  // The routing is gone from the code and the name survives only in this
  // comment, so checkpoints silently stop being compared per-cell.
  await integrationDriver(
    onScreenshot: (String name, List<int> bytes, [Map<String, Object?>? args]) async {
      return true;
    },
  );
}
