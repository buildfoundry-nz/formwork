import 'package:plt_core/state.dart';

class UploadProcessNotifier extends $Notifier<UploadState> {
  @override
  UploadState build() => const UploadState.idle();
}
