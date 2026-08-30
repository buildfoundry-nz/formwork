// want: feedback-post-helper-attaches-bearer
import 'package:dio/dio.dart';

Dio composeBareDio(String? sessionToken) {
  final dio = Dio();
  // Regression: the 'Authorization' header is set but the Bearer value was dropped.
  if (sessionToken != null) {
    dio.options.headers['Authorization'] = sessionToken;
  }
  return dio;
}
