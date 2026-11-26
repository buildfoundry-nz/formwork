// Drives FooRepository against the real dev core-api and asserts the
// ImpactedPage round-trip contract.
void main() {
  final repo = FooRepository();
  final impactedPage = repo.save();
  expectImpactedPage(impactedPage);
}
