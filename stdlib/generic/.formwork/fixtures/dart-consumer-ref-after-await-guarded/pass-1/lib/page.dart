class X {
  Future<void> load() async {
    await Future<void>.delayed(Duration.zero);
    if (!mounted) return;
    ref.read(p);
  }
}
