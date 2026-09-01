void main() {
  tearDownAll(() async {
    try {
      await something();
    } catch (e) {
      fail('reason: $e');
    }
  });
}
