import 'package:feature_dimension/optimistic.dart';

class PartitionHeightWrite extends OptimisticAnnotationWrite<PartitionHeightResponse> {
  PartitionHeightWrite(this.notifier, this.metrics);

  final ProjectCallouts notifier;
  final ProjectTalliesNotifier metrics;

  @override
  void consumeResponse(PartitionHeightResponse resp) {
    notifier.applyImpactedPage(resp.impactedPage); // want: dart-annotation-writes-must-fold-metrics-cache
  }
}
