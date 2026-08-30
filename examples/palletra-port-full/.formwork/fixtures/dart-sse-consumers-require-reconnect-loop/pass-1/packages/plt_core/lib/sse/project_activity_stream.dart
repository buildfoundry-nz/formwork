import 'package:plt_core/sse/sse_retry.dart';

class ProjectActivityStream {
  Stream<ProjectUpdate> subscribe(String projectId) => autoReconnectSseStream(
        connect: (cursor) => _connectOnce(projectId, cursor),
      );

  Future<Response> _connectOnce(String projectId, String? cursor) {
    return _dio.get(
      '/api/projects/$projectId/events',
      options: Options(headers: {'Accept': 'text/event-stream'}),
    );
  }
}
