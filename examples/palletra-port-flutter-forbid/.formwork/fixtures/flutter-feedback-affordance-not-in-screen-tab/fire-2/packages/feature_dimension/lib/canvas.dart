import 'package:flutter/material.dart';

// A constructor usage of a Feedback<...>Tab identifier also fires.
Widget buildOverlay() {
  return const FeedbackEdgeTab(); // want: flutter-feedback-affordance-not-in-screen-tab
}
