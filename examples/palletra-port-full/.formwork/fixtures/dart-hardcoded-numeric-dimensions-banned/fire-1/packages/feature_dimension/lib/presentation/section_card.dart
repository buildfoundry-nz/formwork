import 'package:flutter/material.dart';
import 'package:plt_theme/shell_spacing.dart';

class SectionPanel extends StatelessWidget {
  const SectionPanel({super.key});

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: EdgeInsets.all(ShellSpacing.md),
      child: Text(
        'Section',
        style: TextStyle( // want: dart-hardcoded-numeric-dimensions-banned
          fontSize: 14,
          color: Theme.of(context).colorScheme.onSurface,
        ),
      ),
    );
  }
}
