import 'package:flutter/widgets.dart';

class SummaryRow extends StatelessWidget {
  const SummaryRow({super.key, required this.value});

  final double value;

  @override
  Widget build(BuildContext context) {
    return Text(value.toStringAsFixed(2)); // want: tostringasfixed-site-inventory
  }
}
