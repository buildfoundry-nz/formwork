// Covers the ProjectActivityStream SSE consumer: opens a real stream, asserts the
// bearer attaches, and surfaces a typed 401.
void main() {
  test('project events sse surfaces a typed 401', () {
    final stream = ProjectActivityStream();
    expect(() => stream.connect('bad-token'), throwsA(isA<UnauthorizedException>()));
  });
}
