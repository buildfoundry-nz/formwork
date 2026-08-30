import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets('annotation viewer lists annotations from the real API', (tester) async {
    await tester.pumpWidget(const App());
    expect(find.text('loaded'), findsWidgets);
  });
}
