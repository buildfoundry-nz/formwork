class FrameTimingSampler {
  // Go computes ttfb = responseStart - requestStart and every p50/p95 from the
  // sorted samples; the sampler forwards raw values only and derives nothing.
  void report(FrameTimingSample s) {
    _sink.add(
      RawFrameStats(
        start: s.start,
        end: s.end,
        raster: s.raster,
      ),
    );
  }
}
