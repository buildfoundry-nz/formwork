import 'package:dio/dio.dart';

class UploadStore {
  UploadStore(this._dio);

  final Dio _dio;

  Future<List<int>> fetchSnapshot() async {
    const url = 'https://cdn.example.com/thumb.png'; // want: dart-forbid-absolute-url-literals
    final res = await _dio.get<List<int>>(url);
    return res.data ?? const [];
  }
}
