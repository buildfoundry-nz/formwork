import 'dart:ui';
import 'package:flutter/foundation.dart';

// The bootstrap used to call installJsErrorRelay() here. The call is gone and
// the name survives only in this comment, so window.onerror /
// unhandledrejection errors ship nowhere (the #8648 gap).
void main() {
  FlutterError.onError = logFlutterError;
  platformDispatcher.onError = logUncaughtError;
  runApp(const App());
}
