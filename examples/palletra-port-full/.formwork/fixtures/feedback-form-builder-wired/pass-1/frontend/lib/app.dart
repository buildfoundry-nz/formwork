import 'package:flutter/material.dart';
import 'package:feature_critique/presentation/tailored_feedback_form.dart';

Widget buildApp(BuildContext context) {
  return BetterFeedback(
    feedbackBuilder: critiqueFormBuilder,
    child: MaterialApp(),
  );
}
