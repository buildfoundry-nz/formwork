import 'package:flutter/gestures.dart';
import 'tap_debounce.dart';

class ConduitTool with TapDebounceMixin {
  void onPointerDown(PointerDownEvent event) {
    if (!confirmTap(event)) return;
    dropVertex(event.localPosition);
  }
}
