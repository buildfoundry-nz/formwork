import 'package:plt_core/network/wire_dio.dart';

class UploadStore {
  UploadStore(this._proto);
  final WireDio _proto;

  Future<void> upload(String path) async {
    await _proto.postMessage('/api/uploads', {'path': path});
  }
}
