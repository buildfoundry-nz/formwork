class Repo {
  final Map<String, Future<int>> _inFlight = {}; // want: flutter-single-flight-owned-by-core
}
