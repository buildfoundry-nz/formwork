import 'package:flutter/material.dart';

// The proposal line, dimension box and button swatch.
class SuggestedCalibrationLine extends StatelessWidget {
  const SuggestedCalibrationLine({super.key});

  @override
  Widget build(BuildContext context) {
    // Raw literal — a second color source for the ONE proposal fact.
    return Container(color: const Color(0xFF3366FF)); // want: calibration-proposal-single-color
  }
}
