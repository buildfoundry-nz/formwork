class SessionStoreLru {
  final List<CacheSlot> _entries = [];

  void evictAll() {
    for (final entry in _entries) {
      entry.invalidate();
    }
    _entries.clear();
  }
}
