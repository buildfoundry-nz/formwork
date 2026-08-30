class FrameTimingSampler {
  void report(FrameTimingSample s) {
    final ttfb = s.responseStart - s.requestStart; // want: flutter-perf-values-forwarded-raw
    _sink.add(ttfb);
  }
}
