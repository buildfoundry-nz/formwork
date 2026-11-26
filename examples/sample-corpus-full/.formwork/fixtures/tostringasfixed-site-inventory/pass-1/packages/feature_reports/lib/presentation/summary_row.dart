import 'package:flutter/widgets.dart';
import 'package:plt_core/format/format_compact.dart';

class SummaryRow extends StatelessWidget {
  const SummaryRow({super.key, required this.value});

  final double value;

  @override
  Widget build(BuildContext context) {
    return Text(formatCompact(value));
  }
}
