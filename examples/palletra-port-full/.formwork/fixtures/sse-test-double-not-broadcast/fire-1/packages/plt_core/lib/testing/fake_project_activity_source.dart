import 'dart:async';

import 'package:plt_core/events/project_activity_source.dart';

/// Test double for the SSE project events stream.
class FakeProjectActivitySource implements ProjectActivitySource {
  final StreamController<ProjectUpdate> _controller =
      StreamController<ProjectUpdate>.broadcast(); // want: sse-test-double-not-broadcast

  @override
  Stream<ProjectUpdate> subscribe({String? lastEventId}) => _controller.stream;

  void emit(ProjectUpdate event) => _controller.add(event);
}
