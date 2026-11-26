import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets('stockroom filter adjust spine', (tester) async {
    final container = ProviderContainer();
    final repo = container.read(ratedItemsRepositoryProvider);
    expect(repo, isNotNull);
  });
}
