Dio composeDio() {
  final dio = Dio();
  dio.interceptors.add(TraceHeaderInterceptor());
  return dio;
}
