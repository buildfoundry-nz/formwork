import 'package:flutter_riverpod/flutter_riverpod.dart';

class TuneCalibrationToolLayer {
  // This layer no longer reads suggestedCalibrationProvider directly; it
  // derives from suggestedCalibrationLine so it cannot disagree with the card.
  void build(WidgetRef ref) {
    final line = ref.watch(suggestedCalibrationLineProvider);
    _ = line;
  }
}
