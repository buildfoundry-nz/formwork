class X {
  Future<void> load() async {
    await Future<void>.delayed(Duration.zero);
    ref.read(p); // want: dart-consumer-ref-after-await-guarded
  }
}
