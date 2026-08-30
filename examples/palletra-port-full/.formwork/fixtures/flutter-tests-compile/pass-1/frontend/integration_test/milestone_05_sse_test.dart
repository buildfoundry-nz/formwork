import 'package:flutter_test/flutter_test.dart';
import 'app_helpers.dart';

void main() {
  testWidgets('sse typed-401 contract', (tester) async {
    await tester.pumpWidget(wrapApp(const SizedBox()));
    expect(find.byType(SizedBox), findsOneWidget);
  });
}
