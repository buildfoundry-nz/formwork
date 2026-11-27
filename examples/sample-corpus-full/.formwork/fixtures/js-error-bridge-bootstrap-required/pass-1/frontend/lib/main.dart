import 'dart:ui';
import 'package:flutter/foundation.dart';

import 'package:plt_core/observability/js_error_relay.dart';

// The JS error bridge is installed at the one bootstrap, alongside the Dart
// global handlers.
void main() {
  FlutterError.onError = logFlutterError;
  platformDispatcher.onError = logUncaughtError;
  if (AppConfig.clientTelemetryEnabled) {
    installJsErrorRelay();
  }
  runApp(const App());
}
