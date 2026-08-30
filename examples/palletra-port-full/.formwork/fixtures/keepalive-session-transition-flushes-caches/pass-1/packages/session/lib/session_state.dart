import 'session_store_lru.dart';

Future<SessionState> sessionState(Ref ref) async {
  final auth = await ref.watch(authProvider.future);
  if (auth.loggedOut) {
    flushSessionCaches(ref);
    return const SessionState.loggedOut();
  }
  return SessionState.active(auth.userId);
}
