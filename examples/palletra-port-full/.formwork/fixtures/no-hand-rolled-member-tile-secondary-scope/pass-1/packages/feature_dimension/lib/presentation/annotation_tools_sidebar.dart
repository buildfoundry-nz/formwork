import 'package:flutter/material.dart';

class MarkerToolsSidebar extends StatelessWidget {
  const MarkerToolsSidebar({super.key});

  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    // The IDENTICAL muted-tile shell, but this is the annotation tools sidebar —
    // it renders NO member/invite row type, so the tier-2 member-row invariant
    // keeps it clean.
    return DecoratedBox(
      decoration: BoxDecoration(
        color: colors.surfaceContainerHighest.withValues(alpha: 0.4),
        borderRadius: ShellSpacing.cornerRadiusSm,
      ),
      child: const Padding(
        padding: ShellSpacing.gapMd,
        child: Text('tools'),
      ),
    );
  }
}
