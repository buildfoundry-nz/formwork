import '../data/approval_repository.dart';

class ContinueGate {
  Future<bool> canProceed(WidgetRef ref) async {
    final next = await ref.watch(upcomingUnapprovedProvider.future); // want: no-second-nextunapproved-scan-on-proceed
    return next.pageId.isEmpty;
  }
}
