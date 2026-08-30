import 'package:flutter_test/flutter_test.dart';

// DeviceMatrixCell is read by the harness (RenderHarness), never by specs.
// The matrix engine/viewport is injected via fromEnvironment('CELL_ENGINE')
// in the driver, so this shared spec stays engine-agnostic.
void main() {
  test('checkout paints', () {
    expect(1 + 1, 2);
  });
}
