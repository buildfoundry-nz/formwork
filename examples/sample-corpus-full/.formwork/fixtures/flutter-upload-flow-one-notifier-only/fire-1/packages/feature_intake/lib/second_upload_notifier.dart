import 'package:plt_core/state.dart';

// A second, forking UploadState machine — the exact anti-pattern the gate bans.
class DuplicateUploadNotifier extends Notifier<UploadState> {
  @override
  UploadState build() => const UploadState.idle();
}
