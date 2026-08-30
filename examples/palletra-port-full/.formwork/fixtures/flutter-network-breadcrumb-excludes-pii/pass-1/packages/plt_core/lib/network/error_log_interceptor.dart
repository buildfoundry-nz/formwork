import 'package:dio/dio.dart';

void onError(DioException err) {
  if (err.requestOptions.path == '/api/ux-events') return;
  final method = err.requestOptions.method;
  final status = err.response?.statusCode;
  report('$method $status');
}
