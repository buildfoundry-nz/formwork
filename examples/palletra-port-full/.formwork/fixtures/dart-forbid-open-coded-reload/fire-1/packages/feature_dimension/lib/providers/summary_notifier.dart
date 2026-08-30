import 'package:feature_dimension/data.dart';

class OverviewNotifier extends _$OverviewNotifier {
  Future<void> refreshSummary() async {
    state = AsyncData(await _repo.fetchRecap()); // want: dart-forbid-open-coded-reload
  }
}
