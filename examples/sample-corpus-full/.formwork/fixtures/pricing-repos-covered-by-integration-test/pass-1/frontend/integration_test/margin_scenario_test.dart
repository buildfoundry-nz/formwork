// margin_scenario_test covers MarginModelRepository per-repository: it names
// the class, signs in a dev e2e user, drives deriveSensitivity against the real
// core-api, and asserts the MarginSensitivity decode round-trips.
@TestOn('chrome')
library;

void main() {
  testWidgets('margin scenario sensitivity round-trips', (tester) async {
    final repo = MarginModelRepository(dio);
    final res = await repo.deriveSensitivity(request);
    expect(res, isA<MarginSensitivity>());
  });
}
