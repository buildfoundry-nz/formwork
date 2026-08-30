import 'package:flutter_test/flutter_test.dart';

void main() {
  test('checkout paints', () {
    final cell = DeviceMatrixCell.fromEnvironment(); // want: cross-browser-spec-no-cell-branching
    expect(cell, isNotNull);
  });
}
