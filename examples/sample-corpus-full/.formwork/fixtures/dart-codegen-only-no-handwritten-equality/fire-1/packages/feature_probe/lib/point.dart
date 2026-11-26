class Point {
  final int x;
  const Point(this.x);
  bool operator ==(Object other) => other is Point && other.x == x; // want: dart-codegen-only-no-handwritten-equality
}
