import 'dart:ui' as ui;

/// A decoded-bitmap cache that disposes its handle on flush AND hands the
/// same handle out to painters — a use-after-dispose crash waiting to happen.
class PageRenderCache {
  ui.Image? _current;

  void store(ui.Image image) {
    _current?.dispose();
    _current = image;
  }

  void flush() {
    _current?.dispose();
    _current = null;
  }

  // Raw exposure of the cache's private handle — no .clone(). The next
  // flush() frees it out from under the painter.
  ui.Image? get current => _current;
}
