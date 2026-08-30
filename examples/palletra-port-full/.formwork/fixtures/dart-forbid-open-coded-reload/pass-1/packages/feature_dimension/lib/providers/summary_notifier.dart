import 'package:plt_core/refetch_in_place.dart';

class OverviewNotifier extends _$OverviewNotifier {
  Future<void> refreshSummary() async {
    final fresh = await _reload.latest(() => _repo.fetchRecap());
    if (fresh == null) return;
    state = AsyncData(fresh);
  }
}
