import 'package:flutter_test/flutter_test.dart';

void main() {
  test('surfaces the failure', () async {
    try {
      await something();
    } catch (e) {
      fail('cleanup threw: $e');
    }
  });
}
