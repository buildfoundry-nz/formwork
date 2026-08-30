import 'package:flutter/widgets.dart';

Widget railPanel(String kind) {
  if (kind == 'tie_confirm_all') { // want: flutter-rail-bulk-action-single-contract
    return const Text('Confirm all tie codes'); // want: flutter-rail-bulk-action-single-contract
  }
  return const SizedBox.shrink();
}
