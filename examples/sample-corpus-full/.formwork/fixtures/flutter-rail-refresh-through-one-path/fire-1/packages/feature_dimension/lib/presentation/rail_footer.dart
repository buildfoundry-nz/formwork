import 'package:flutter_riverpod/flutter_riverpod.dart';

void onEndorse(WidgetRef ref) {
  ref.invalidate(plotPrimaryActionProvider); // want: flutter-rail-refresh-through-one-path
}
