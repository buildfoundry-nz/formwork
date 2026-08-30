// Covers only the jobs-queue live-events consumer. The project stream consumer
// ships with no coverage here.
void main() {
  test('jobs queue sse surfaces a typed 401', () {
    final stream = TasksQueueEventsStream();
    expect(() => stream.connect('bad-token'), throwsA(isA<UnauthorizedException>()));
  });
}
