// The Flutter (Dart) replacement for the deleted TypeScript widget. Dart and Go
// are the only permitted source extensions in this repo.
int buildReport(List<int> rows) {
  return rows.fold(0, (a, b) => a + b);
}
