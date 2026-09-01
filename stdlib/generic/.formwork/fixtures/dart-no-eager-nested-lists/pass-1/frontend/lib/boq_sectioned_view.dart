import 'package:flutter/material.dart';

class BoqSectionedView extends StatelessWidget {
  const BoqSectionedView({super.key, required this.lines});

  final List<Widget> lines;

  @override
  Widget build(BuildContext context) {
    // A small bounded panel is an honest Column — no eager-lazy masquerade.
    return Column(
      mainAxisSize: MainAxisSize.min,
      children: lines,
    );
  }
}

class BigList extends StatelessWidget {
  const BigList({super.key, required this.lines});

  final List<Widget> lines;

  @override
  Widget build(BuildContext context) {
    // A viewport-owned lazy list: scrolls itself, no shrinkWrap.
    return ListView.builder(
      itemCount: lines.length,
      itemBuilder: (context, index) => lines[index],
    );
  }
}
