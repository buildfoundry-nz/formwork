import 'dart:async';

import 'package:plt_core/events/project_activity_source.dart';

/// Single-subscription replay double: buffers events and replays them on
/// subscribe so pre-subscribe events are not dropped.
class FakeProjectActivitySource implements ProjectActivitySource {
  final List<ProjectUpdate> _buffer = <ProjectUpdate>[];
  StreamController<ProjectUpdate>? _controller;

  @override
  Stream<ProjectUpdate> subscribe({String? lastEventId}) {
    final controller = StreamController<ProjectUpdate>();
    for (final event in _buffer) {
      controller.add(event);
    }
    _controller = controller;
    return controller.stream;
  }

  void emit(ProjectUpdate event) {
    _buffer.add(event);
    _controller?.add(event);
  }
}
