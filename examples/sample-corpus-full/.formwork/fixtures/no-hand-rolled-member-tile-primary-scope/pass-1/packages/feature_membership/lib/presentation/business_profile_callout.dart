import 'package:flutter/material.dart';

class AccountProfileCallout extends StatelessWidget {
  const AccountProfileCallout({super.key});

  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    // primaryContainer + cornerRadiusMd, no alpha-muted surface + cornerRadiusSm
    // pair — the legit non-member callout the shell fingerprint must not catch.
    return DecoratedBox(
      decoration: BoxDecoration(
        color: colors.primaryContainer,
        borderRadius: ShellSpacing.cornerRadiusMd,
      ),
      child: const Text('Complete your business profile'),
    );
  }
}
