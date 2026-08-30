class SessionStoreLru {
  final List<CacheSlot> _entries = [];

  void evictAll() { // want: keepalive-evictall-invalidates-watched-cache
    for (final entry in _entries) {
      entry.close();
    }
    _entries.clear();
  }
}
