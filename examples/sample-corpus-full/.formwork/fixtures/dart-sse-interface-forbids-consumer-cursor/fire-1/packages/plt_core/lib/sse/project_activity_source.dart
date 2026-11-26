// The SSE event-source interface exposes a per-consumer resume cursor through
// subscribe() — a Liskov violation the multiplexing decorator cannot honour.
abstract interface class ProjectActivitySource { // want: dart-sse-interface-forbids-consumer-cursor
  Stream<ProjectUpdate> subscribe(String projectId, {String? lastEventId});

  Future<void> close();
}
