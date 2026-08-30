// Measure feature renders the server-formatted scalar value verbatim.
import 'package:flutter/material.dart';

class ScalarMaskRow extends StatelessWidget {
  const ScalarMaskRow({super.key, required this.step});

  final WorkflowStage step;

  @override
  Widget build(BuildContext context) {
    return Text(step.scalarValue);
  }
}

class WorkflowStage {
  const WorkflowStage({required this.title, required this.scalarValue});
  final String title;
  final String scalarValue;
}
