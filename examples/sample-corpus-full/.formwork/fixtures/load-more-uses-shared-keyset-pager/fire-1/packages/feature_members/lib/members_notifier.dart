import 'package:flutter_riverpod/flutter_riverpod.dart';

class CrewNotifier extends AsyncNotifier<CrewState> {
  Future<void> fetchMore() async { // want: load-more-uses-shared-keyset-pager
    final current = state.requireValue;
    if (current.fetchingMore) return;
    state = AsyncData(current.copyWith(fetchingMore: true));
    final page = await _api.fetchOrgMembers(after: current.nextPageToken);
    state = AsyncData(current.copyWith(
      members: [...current.members, ...page.members],
      nextPageToken: page.nextPageToken,
      fetchingMore: false,
    ));
  }
}
