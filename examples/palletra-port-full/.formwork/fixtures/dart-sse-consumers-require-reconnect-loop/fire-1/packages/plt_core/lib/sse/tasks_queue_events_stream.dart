import 'package:plt_core/network.dart';

class TasksQueueEventsStream {
  Stream<JobEvent> subscribe(String projectId) async* {
    while (true) {
      final res = await _dio.get(
        '/api/jobs/queue/events',
        options: Options(headers: {'Accept': 'text/event-stream'}), // want: dart-sse-consumers-require-reconnect-loop
      );
      await for (final e in res.stream) {
        yield e;
      }
      return;
    }
  }
}
