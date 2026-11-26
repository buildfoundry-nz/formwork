import 'session_store_lru.dart';

/// Flushes every per-project cache on sign-out.
void flushSessionCaches(WidgetRef ref) { // want: lru-drained-on-caches-flush
  // BUG: only clears one cache instead of draining the whole LRU registry.
  ref.invalidate(markerGraphProvider);
}
