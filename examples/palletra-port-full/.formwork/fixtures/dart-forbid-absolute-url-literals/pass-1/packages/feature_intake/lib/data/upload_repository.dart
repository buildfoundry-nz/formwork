import 'package:dio/dio.dart';

class UploadStore {
  UploadStore(this._dio);

  final Dio _dio;

  Future<List<int>> fetchSnapshot() async {
    const path = '/api/page-renders/1/thumb.png';
    final res = await _dio.get<List<int>>(path);
    return res.data ?? const [];
  }
}
