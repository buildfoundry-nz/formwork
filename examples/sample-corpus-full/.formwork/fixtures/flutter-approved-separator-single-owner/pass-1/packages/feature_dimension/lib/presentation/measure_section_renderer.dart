import 'package:flutter/widgets.dart';

Widget composeSection(List<Unit> units, Widget Function(Unit) card) {
  return Column(children: composeApprovalPartitionedCards(units, card: card));
}
