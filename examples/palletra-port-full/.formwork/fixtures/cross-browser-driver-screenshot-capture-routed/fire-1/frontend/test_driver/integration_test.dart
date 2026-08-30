import 'package:integration_test/integration_test_driver_extended.dart';

Future<void> main() async {
  // BUG: screenshots are taken directly and never routed through the
  // cross-browser handler, so checkpoints stop being compared per-cell.
  await integrationDriver(
    onScreenshot: (String name, List<int> bytes, [Map<String, Object?>? args]) async {
      return true;
    },
  );
}
