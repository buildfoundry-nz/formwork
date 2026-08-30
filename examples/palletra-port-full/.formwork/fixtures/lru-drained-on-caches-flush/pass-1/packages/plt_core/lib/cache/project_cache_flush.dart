import 'session_store_lru.dart';

/// Flushes every per-project cache on sign-out.
void flushSessionCaches(WidgetRef ref) {
  SessionStoreLru.instance.evictAll(ref);
}
