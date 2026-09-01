@riverpod
class Foo extends _$Foo {
  bool build() => true;
  Future<void> save() async {
    final link = ref.keepAlive();
    try {
      await repo.save();
    } finally {
      link.close();
    }
  }
}
