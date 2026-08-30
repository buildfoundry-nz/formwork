import 'package:flutter/material.dart';
import 'package:plt_widgets/plt_widgets.dart';

class StatusTag extends StatelessWidget {
  const StatusTag({super.key, required this.label, required this.tone});
  final String label;
  final Color tone;

  @override
  Widget build(BuildContext context) {
    return ToneTag.tint(label: label, tone: tone);
  }
}
