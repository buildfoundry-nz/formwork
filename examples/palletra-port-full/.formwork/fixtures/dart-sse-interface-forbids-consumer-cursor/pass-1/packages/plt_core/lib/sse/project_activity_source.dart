// The SSE event-source interface's subscribe() takes only its positional
// required params — no per-consumer cursor knob. The reconnect cursor is
// server-driven inside autoReconnectSseStream.
abstract interface class ProjectActivitySource {
  Stream<ProjectUpdate> subscribe(String projectId);

  Future<void> close();
}
