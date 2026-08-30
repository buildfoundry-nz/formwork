import 'package:flutter/material.dart';

// The half-port shape: render the label with a trailing em-dash placeholder
// instead of the real server value.
Widget scalarMask(step) {
  return Text('${step.title} —'); // want: flutter-no-placeholder-mask-label
}
