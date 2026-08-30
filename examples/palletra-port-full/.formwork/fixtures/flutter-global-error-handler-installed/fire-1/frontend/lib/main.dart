import 'dart:ui';

// The async channel is handled but the SYNCHRONOUS build/layout/paint channel
// is left at Flutter's default — the exact #2961 regression this gate catches.
void main() {
  platformDispatcher.onError = logUncaughtError;
  runApp(const App());
}
