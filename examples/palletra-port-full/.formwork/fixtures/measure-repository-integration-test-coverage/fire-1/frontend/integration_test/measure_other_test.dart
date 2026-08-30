// Drives a different wire repository against the real backend, leaving the Bar
// wire repo with no integration test naming it (#6479 per-repository coverage).
void main() {
  final repo = AuxRepository();
  repo.save(); // asserts impactedPage
}
