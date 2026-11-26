import 'package:plt_core/optimistic.dart';

// Rides the SpeculativeWrite<T> contract: importing optimistic.dart is the
// file-level acknowledgement that the eight-step flow runs through .run().
class WorkflowPanelController extends SpeculativeWrite<Draft> {
  WorkflowPanelController(this.wire, this.cache);
  final Wire wire;
  final Cache cache;

  Future<void> approve(Draft draft) async {
    final token = applySpeculative(draft);
    final res = await wire.approve(draft);
    cache.consumeResponse(token, res);
  }
}
