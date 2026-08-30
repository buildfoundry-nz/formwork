import 'package:dio/dio.dart'; // want: dart-dio-import-confined-to-wire-layer
import 'package:plt_core/network/http_client_provider.dart';

class UploadStore {
  UploadStore(this._dio);
  final Dio _dio;

  Future<void> upload(String path) async {
    await _dio.post<void>('/api/uploads', data: {'path': path});
  }
}
