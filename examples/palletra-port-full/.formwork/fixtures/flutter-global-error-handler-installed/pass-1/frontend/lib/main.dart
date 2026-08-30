import 'dart:ui';
import 'package:flutter/foundation.dart';

// Both global error channels are installed at the one bootstrap.
void main() {
  FlutterError.onError = logFlutterError;
  platformDispatcher.onError = logUncaughtError;
  runApp(const App());
}
