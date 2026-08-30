import 'package:dio/dio.dart';

// The wire layer (plt_core/lib/network/) legitimately composes Dio internals;
// this file is outside the enforced surface, so the direct dio import is fine.
Dio composeMainDio() {
  return Dio(BaseOptions(baseUrl: '/api'));
}
