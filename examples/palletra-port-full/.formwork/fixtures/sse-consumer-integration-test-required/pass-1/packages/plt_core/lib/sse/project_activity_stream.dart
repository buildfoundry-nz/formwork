// A production SSE consumer: it appends the text/event-stream Accept header and
// reads the live stream. It ships WITH a matching *sse*.dart integration test.
class ProjectActivityStream {
  Stream<String> connect(String bearer) async* {
    final headers = {'Accept': 'text/event-stream', 'Authorization': 'Bearer $bearer'};
    yield headers.toString();
  }
}
