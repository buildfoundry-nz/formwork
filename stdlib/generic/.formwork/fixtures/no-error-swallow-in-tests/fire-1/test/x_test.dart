void main() {
  tearDownAll(() async {
    try {
      await something();
    } catch (_) {} // want: no-error-swallow-in-tests
  });
}
