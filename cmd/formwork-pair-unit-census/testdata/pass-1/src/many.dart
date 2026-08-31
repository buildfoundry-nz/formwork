// Dart-only multi-trigger evidence under an explicit same-file arm.
// Dummy db: same reason as fire-1/src/locks.dart. Two trigger calls.
class _Db {
  const _Db();
  void lockThing() {}
}

const db = _Db();

void f() {
  db.lockThing();
  db.lockThing();
}
