import 'package:flutter/material.dart';
import 'package:plt_widgets/fields_required_chip.dart';

class PricingActionBar extends StatelessWidget {
  const PricingActionBar({super.key, required this.count});
  final int count;

  @override
  Widget build(BuildContext context) {
    // An error_outline icon used elsewhere, but NO errorContainer pill fill —
    // the shared FieldsRequiredChip owns the pill.
    return Row(
      children: [
        const Icon(Icons.error_outline),
        FieldsRequiredChip(count: count),
      ],
    );
  }
}
