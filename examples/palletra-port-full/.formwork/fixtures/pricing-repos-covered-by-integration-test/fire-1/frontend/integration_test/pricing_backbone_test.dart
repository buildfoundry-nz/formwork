// pricing_backbone_test drives the CostingRepository BOM document round-trip against
// the real core-api. It covers only the spine repo and names no other pricing
// repository, so the margin-scenario sibling ships uncovered under a green gate
// when the gate accepts any one pricing test (#6840).
@TestOn('chrome')
library;

void main() {
  testWidgets('pricing spine round-trips the active BOM', (tester) async {
    final repo = CostingRepository(dio);
    final res = await repo.getActiveBom(projectId);
    expect(res, isA<GetActiveBomResponse>());
  });
}
