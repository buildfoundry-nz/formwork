import 'package:flutter/widgets.dart';

Widget composeSection(List<Widget> approved) {
  return Column(children: [
    ClearedSeparator(count: approved.length), // want: flutter-approved-separator-single-owner
    ...approved,
  ]);
}
