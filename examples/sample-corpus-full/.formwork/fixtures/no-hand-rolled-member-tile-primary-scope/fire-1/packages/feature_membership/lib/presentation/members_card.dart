import 'package:flutter/material.dart';

class PendingTile extends StatelessWidget {
  const PendingTile({super.key});

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return DecoratedBox(
      decoration: BoxDecoration( // want: no-hand-rolled-member-tile-primary-scope
        color: scheme.surfaceContainerHigh.withValues(alpha: 0.35),
        borderRadius: ShellSpacing.cornerRadiusSm,
      ),
      child: const Padding(
        padding: ShellSpacing.gapLg,
        child: Text('pending invite'),
      ),
    );
  }
}
