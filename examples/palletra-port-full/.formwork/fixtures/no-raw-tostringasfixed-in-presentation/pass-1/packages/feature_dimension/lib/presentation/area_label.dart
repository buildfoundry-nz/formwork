import 'package:flutter/widgets.dart';
import 'package:plt_core/format/format.dart';

class ExtentLabel extends StatelessWidget {
  const ExtentLabel(this.area, {super.key});

  final double area;

  @override
  Widget build(BuildContext context) {
    return Text(formatDimension(area, unit: 'm2'));
  }
}
