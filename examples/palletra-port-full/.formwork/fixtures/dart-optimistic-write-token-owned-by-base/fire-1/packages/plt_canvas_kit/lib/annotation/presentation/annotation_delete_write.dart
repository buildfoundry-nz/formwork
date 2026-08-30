// A leaf annotation write that hand-rolls the freshness token instead of
// letting the OptimisticAnnotationWrite base own it — audit-B token leak.
class MarkerDeleteWrite extends OptimisticAnnotationWrite<RemoveAnnotationResponse> { // want: dart-optimistic-write-token-owned-by-base
  MarkerDeleteWrite(this.ref, this.id);

  final Ref ref;
  final String id;

  @override
  void applySpeculativePatch(MarkerState s) {
    final token = ledger.enrolMutation(id);
    s.remove(id);
    _token = token;
  }

  @override
  Future<RemoveAnnotationResponse> call() => api.delete(id);
}
