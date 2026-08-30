import 'package:flutter/gestures.dart';

class ConduitTool {
  static const _debounceWindow = Duration(milliseconds: 80); // want: dart-forbid-open-coded-tap-dedup

  void onPointerDown(PointerDownEvent event) {
    dropVertex(event.localPosition);
  }
}
