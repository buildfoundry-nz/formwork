// A conformant leaf: extends OptimisticAnnotationWrite and implements only the
// subclass hooks. The base owns register/isNewest/release — the leaf never
// touches the token plumbing.
class MarkerDeleteWrite
    extends OptimisticAnnotationWrite<RemoveAnnotationResponse> {
  MarkerDeleteWrite(this.ref, this.id);

  final Ref ref;
  final String id;

  @override
  void applySpeculativePatch(MarkerState s) {
    s.remove(id);
  }

  @override
  Future<RemoveAnnotationResponse> call() => api.delete(id);

  @override
  void consumeResponse(RemoveAnnotationResponse r, MarkerState s) {
    s.markRemoved(id);
  }

  @override
  void rollback(MarkerState s) {
    s.restore(id);
  }
}
