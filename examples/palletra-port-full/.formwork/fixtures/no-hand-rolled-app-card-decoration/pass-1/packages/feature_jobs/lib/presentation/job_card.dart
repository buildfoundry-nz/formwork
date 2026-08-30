import 'package:flutter/material.dart';
import 'package:plt_widgets/shell_card.dart';

class DutyCard extends StatelessWidget {
  const DutyCard({super.key});

  @override
  Widget build(BuildContext context) {
    final colorScheme = Theme.of(context).colorScheme;
    // A rounded fill with NO Border.all — two of the three fingerprints only.
    // Real card surfaces route through ShellCard.
    return ShellCard(
      child: DecoratedBox(
        decoration: BoxDecoration(
          color: colorScheme.surfaceContainerLow,
          borderRadius: ShellSpacing.cornerRadiusLg,
        ),
        child: const Text('job'),
      ),
    );
  }
}
