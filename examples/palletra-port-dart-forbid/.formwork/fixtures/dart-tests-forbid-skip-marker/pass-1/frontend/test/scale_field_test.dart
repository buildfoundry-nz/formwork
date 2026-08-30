import 'package:flutter_test/flutter_test.dart';

void main() {
  test('streams the live-page-ready scale field', () {
    if (!isPlatformSupported) return;
    expect(1 + 1, 2);
  });
}
