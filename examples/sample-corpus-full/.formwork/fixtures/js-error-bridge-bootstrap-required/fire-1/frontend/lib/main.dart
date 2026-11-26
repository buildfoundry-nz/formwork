import 'dart:ui';
import 'package:flutter/foundation.dart';

// The Dart global handlers are installed, but the JS error bridge is never
// wired at the bootstrap, so window.onerror / unhandledrejection ship nowhere
// (the #8648 gap).
void main() {
  FlutterError.onError = logFlutterError;
  platformDispatcher.onError = logUncaughtError;
  runApp(const App());
}
