Dio composeDio() {
  final dio = Dio();
  // The main dio used to add TraceHeaderInterceptor() here. The install is gone
  // from the code and the name survives only in this comment, so outbound calls
  // carry no traceparent and the server span never parents under the FE trace.
  dio.interceptors.add(TokenInterceptor());
  return dio;
}
