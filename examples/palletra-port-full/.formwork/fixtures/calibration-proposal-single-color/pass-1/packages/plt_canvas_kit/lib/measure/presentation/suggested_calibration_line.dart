import 'package:flutter/material.dart';

class SuggestedCalibrationLine extends StatelessWidget {
  const SuggestedCalibrationLine({super.key});

  @override
  Widget build(BuildContext context) {
    // Resolve the ONE token; no raw literal, no colorScheme role.
    final baseColors = Theme.of(context).extension<AppColors>()!;
    return Container(color: baseColors.adjustmentProposal);
  }
}
