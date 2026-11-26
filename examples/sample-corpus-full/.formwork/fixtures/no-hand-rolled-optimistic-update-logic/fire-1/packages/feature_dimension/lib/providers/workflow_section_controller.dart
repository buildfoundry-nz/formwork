import 'package:plt_core/state.dart';

// Hand-rolled optimistic write: applies an optimistic mutation and patches the
// cache, but neither imports the SpeculativeWrite<T> contract nor carries the
// per-callsite sentinel. This is the pre-T4.3 racy shape.
class WorkflowPanelController {
  WorkflowPanelController(this.wire, this.cache);
  final Wire wire;
  final Cache cache;

  Future<void> approve(Draft draft) async {
    final token = applySpeculative(draft); // want: no-hand-rolled-optimistic-update-logic
    final res = await wire.approve(draft);
    cache.consumeResponse(token, res);
  }
}
