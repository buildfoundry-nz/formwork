import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets('annotation viewer', (tester) async {
    markerRepositoryProvider.overrideWith((ref) => _fake); // want: integration-tests-forbid-mocked-wire-providers
    await tester.pumpWidget(const App());
  });
}
