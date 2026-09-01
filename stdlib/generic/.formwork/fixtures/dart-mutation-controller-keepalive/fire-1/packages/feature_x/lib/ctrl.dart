@riverpod
class Foo extends _$Foo {
  bool build() => true;
  Future<void> save() async {
    await repo.save(); // want: dart-mutation-controller-keepalive
  }
}
