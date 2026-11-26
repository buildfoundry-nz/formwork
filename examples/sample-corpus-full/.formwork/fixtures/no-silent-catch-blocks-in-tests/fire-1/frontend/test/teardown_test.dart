import 'package:flutter_test/flutter_test.dart';

void main() {
  test('swallows the failure', () async {
    try {
      await something();
    } catch (_) {} // want: no-silent-catch-blocks-in-tests
  });
}
