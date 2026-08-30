import 'package:flutter_riverpod/flutter_riverpod.dart';

class TuneCalibrationToolLayer {
  void build(WidgetRef ref) {
    final suggestion = ref.watch(suggestedCalibrationProvider); // want: single-consumer-calibration-suggestion-provider
    _ = suggestion;
  }
}
