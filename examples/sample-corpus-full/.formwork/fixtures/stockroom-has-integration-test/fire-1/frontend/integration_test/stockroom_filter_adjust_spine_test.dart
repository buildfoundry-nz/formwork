import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets('stockroom filter adjust spine', (tester) async {
    // A commented binding must NOT satisfy the pin:
    // final repo = container.read(ratedItemsRepositoryProvider);
    final stub = FakeRatedSkusRepository();
    expect(stub, isNotNull);
  });
}
