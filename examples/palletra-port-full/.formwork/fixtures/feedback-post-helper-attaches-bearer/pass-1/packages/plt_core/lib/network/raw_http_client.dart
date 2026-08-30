import 'package:dio/dio.dart';

Dio composeBareDio(String? sessionToken) {
  final dio = Dio();
  if (sessionToken != null) {
    dio.options.headers['Authorization'] = 'Bearer $sessionToken';
  }
  return dio;
}
