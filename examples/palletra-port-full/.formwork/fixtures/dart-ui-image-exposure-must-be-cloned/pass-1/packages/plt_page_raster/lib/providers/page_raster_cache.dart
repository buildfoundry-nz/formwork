import 'dart:ui' as ui;

/// A decoded-bitmap cache that disposes its private handle on flush but hands
/// out fresh clones, so a painter never holds a handle the cache can free.
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

  // Ownership boundary: expose a fresh clone, un-laundered. The cache frees
  // only its private handle.
  ui.Image? get current => _current?.clone();
}
