import 'dart:ui';

// A pasted second copy of the shared helper — the exact drift the sweep bans.
double _pointToSegmentDistance(Offset p, Offset a, Offset b) {
  final dx = b.dx - a.dx;
  final dy = b.dy - a.dy;
  final t = ((p.dx - a.dx) * dx + (p.dy - a.dy) * dy) / (dx * dx + dy * dy);
  final cx = a.dx + t * dx;
  final cy = a.dy + t * dy;
  return (Offset(cx, cy) - p).distance;
}
