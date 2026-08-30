import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

void main() {
  test('asserts only Riverpod framework behavior', () {
    final container = ProviderContainer();
    expect(container, isNotNull);
  });
}
