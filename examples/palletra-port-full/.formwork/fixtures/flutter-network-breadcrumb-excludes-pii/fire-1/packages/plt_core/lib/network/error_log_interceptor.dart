import 'package:dio/dio.dart';

void onError(DioException err) {
  final q = err.requestOptions.queryParameters; // want: flutter-network-breadcrumb-excludes-pii
  report(q.toString());
}
