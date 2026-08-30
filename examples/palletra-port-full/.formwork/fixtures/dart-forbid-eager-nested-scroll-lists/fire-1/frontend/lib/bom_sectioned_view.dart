import 'package:flutter/material.dart';

class BomSectionedView extends StatelessWidget {
  const BomSectionedView({super.key, required this.lines});

  final List<Widget> lines;

  @override
  Widget build(BuildContext context) {
    return ListView.builder( // want: dart-forbid-eager-nested-scroll-lists
      shrinkWrap: true,
      physics: const NeverScrollableScrollPhysics(),
      itemCount: lines.length,
      itemBuilder: (context, index) => lines[index],
    );
  }
}
