// tone-pill fire fixture: four pill tokens within a 14-line window
import 'package:flutter/material.dart';
import 'package:plt_theme/plt_theme.dart';

class StatusTag extends StatelessWidget {
  const StatusTag({super.key, required this.label, required this.tone});
  final String label;
  final Color tone;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: ShellSpacing.gapChip, // want: no-hand-rolled-tone-pill-widget
      decoration: BoxDecoration(
        color: tone.withValues(alpha: 0.12),
        borderRadius: ShellSpacing.cornerRadiusSm,
      ),
      child: Text(label),
    );
  }
}
