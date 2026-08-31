// Three trigger lines. Dummy db keeps repo-root flutter analyze clean —
// dart-analysis-options-exclude-pinned forbids growing the exclude list
// for fixture props. The method declaration is not a trigger call.
class _Db {
  const _Db();
  void lockThing() {}
}

const db = _Db();

void f() {
  db.lockThing();
  db.lockThing();
  db.lockThing();
}
