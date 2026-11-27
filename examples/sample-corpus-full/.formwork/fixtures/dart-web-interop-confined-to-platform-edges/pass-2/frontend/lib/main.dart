import 'dart:html';
import 'package:flutter/widgets.dart';

void main() {
  // frontend/lib/main.dart is an allowlisted platform edge: the confined
  // web-interop import here is exempt and must NOT trip the rule.
  runApp(const _App());
}
