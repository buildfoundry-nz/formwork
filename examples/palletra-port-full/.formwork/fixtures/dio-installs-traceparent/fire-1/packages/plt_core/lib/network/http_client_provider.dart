Dio composeDio() {
  final dio = Dio();
  dio.interceptors.add(TokenInterceptor());
  return dio;
}
