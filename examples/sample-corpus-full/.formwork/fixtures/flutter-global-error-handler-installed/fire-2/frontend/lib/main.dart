import 'dart:ui';

// The sync-channel install `FlutterError.onError = logFlutterError;` was
// dropped from the bootstrap and now survives only in this comment, so
// build/layout/paint errors bypass package:logging exactly as in #2961.
void main() {
  platformDispatcher.onError = logUncaughtError;
  runApp(const App());
}
