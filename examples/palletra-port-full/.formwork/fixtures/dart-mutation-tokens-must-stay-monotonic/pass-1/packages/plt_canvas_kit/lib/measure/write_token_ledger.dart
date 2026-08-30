/// Issues freshness tokens for in-flight canvas mutations.
class WriteTokenLedger {
  int _next = 0;
  final Map<String, int> _tokens = {};

  int begin(String id) {
    final next = ++_next;
    _tokens[id] = next;
    return next;
  }

  bool isNewest(String id, int token) => _tokens[id] == token;
}
