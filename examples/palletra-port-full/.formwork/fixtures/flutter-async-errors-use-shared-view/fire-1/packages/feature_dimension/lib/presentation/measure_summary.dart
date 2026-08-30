import 'package:flutter/widgets.dart';

class MeasureSummary extends StatelessWidget {
  const MeasureSummary({super.key});

  @override
  Widget build(BuildContext context) {
    final async = ref.watch(measureSummaryProvider);
    return async.when(
      data: (v) => Text(v.title),
      loading: () => const SizedBox(),
      error: (err, st) => const SizedBox(),
    );
  }
}
