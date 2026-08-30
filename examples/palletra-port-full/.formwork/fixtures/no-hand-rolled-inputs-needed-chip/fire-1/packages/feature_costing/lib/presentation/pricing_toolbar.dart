// want: no-hand-rolled-inputs-needed-chip
import 'package:flutter/material.dart';

class PricingActionBar extends StatelessWidget {
  const PricingActionBar({super.key, required this.count});
  final int count;

  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    return Tooltip(
      message: '$count inputs needed',
      child: InkWell(
        onTap: () {},
        child: Container(
          color: colors.errorContainer,
          child: Row(
            children: [
              const Icon(Icons.error_outline),
              Text('$count inputs needed'),
            ],
          ),
        ),
      ),
    );
  }
}
