import 'package:flutter/material.dart';

// The canonical appbar affordance — ends in Button, not Tab. Must NOT fire.
class CritiqueButton extends StatelessWidget {
  const CritiqueButton({super.key});

  @override
  Widget build(BuildContext context) =>
      IconButton(icon: const Icon(Icons.bug_report_outlined), onPressed: () {});
}
