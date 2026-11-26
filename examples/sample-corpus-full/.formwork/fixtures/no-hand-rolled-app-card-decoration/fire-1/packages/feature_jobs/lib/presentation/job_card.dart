import 'package:flutter/material.dart';

class DutyCard extends StatelessWidget {
  const DutyCard({super.key});

  @override
  Widget build(BuildContext context) {
    final colorScheme = Theme.of(context).colorScheme;
    // Tokens written color -> border -> borderRadius (the sweep-8 #9 order the
    // fixed-order regex missed); order-independent lookaheads still fire.
    return Container(
      decoration: BoxDecoration( // want: no-hand-rolled-app-card-decoration
        color: colorScheme.surfaceContainerLow,
        border: Border.all(color: colorScheme.outlineVariant),
        borderRadius: ShellSpacing.cornerRadiusLg,
      ),
      child: const Text('job'),
    );
  }
}
