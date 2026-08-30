import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:plt_core/pagination/keyset_page.dart';

class CrewNotifier extends AsyncNotifier<CrewState> {
  Future<void> fetchMore() async {
    await keysetFetchMore<Member, CrewState>(
      ref: ref,
      current: state.requireValue,
      fetch: (cursor) => _api.fetchOrgMembers(after: cursor),
    );
  }
}
