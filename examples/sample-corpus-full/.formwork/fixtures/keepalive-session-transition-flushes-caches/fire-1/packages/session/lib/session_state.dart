import 'session_store_lru.dart';

Future<SessionState> sessionState(Ref ref) async { // want: keepalive-session-transition-flushes-caches
  final auth = await ref.watch(authProvider.future);
  if (auth.loggedOut) {
    return const SessionState.loggedOut();
  }
  return SessionState.active(auth.userId);
}
