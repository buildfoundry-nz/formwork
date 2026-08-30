import 'package:flutter/material.dart';

Color dotColor(AnnotationType type) {
  return MarkerTypeColors.stroke(type); // want: flutter-metric-dot-color-via-shared-resolver
}
