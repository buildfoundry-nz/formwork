import 'package:flutter/material.dart';
import 'package:plt_core/network/wire_dio.dart';

// A base package importing another BASE package — the arrow points downward,
// which is allowed.
class PlotChip extends StatelessWidget {
  const PlotChip({super.key, required this.proto});

  final WireDio proto;

  @override
  Widget build(BuildContext context) {
    return const SizedBox.shrink();
  }
}
