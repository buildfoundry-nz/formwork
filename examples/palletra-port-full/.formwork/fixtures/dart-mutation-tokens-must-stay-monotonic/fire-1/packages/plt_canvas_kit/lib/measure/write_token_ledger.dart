/// Issues freshness tokens for in-flight canvas mutations.
class WriteTokenLedger {
  final Map<String, int> _tokens = {};

  int begin(String id) {
    final next = (_tokens.remove(id) ?? 0) + 1; // want: dart-mutation-tokens-must-stay-monotonic
    _tokens[id] = next;
    return next;
  }

  bool isNewest(String id, int token) => _tokens[id] == token;
}
