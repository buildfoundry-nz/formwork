import 'package:flutter/material.dart';

class BoqSectionedView extends StatelessWidget {
  const BoqSectionedView({super.key, required this.lines});

  final List<Widget> lines;

  @override
  Widget build(BuildContext context) {
    return ListView.builder( // want: dart-no-eager-nested-lists
      shrinkWrap: true,
      physics: const NeverScrollableScrollPhysics(),
      itemCount: lines.length,
      itemBuilder: (context, index) => lines[index],
    );
  }
}
