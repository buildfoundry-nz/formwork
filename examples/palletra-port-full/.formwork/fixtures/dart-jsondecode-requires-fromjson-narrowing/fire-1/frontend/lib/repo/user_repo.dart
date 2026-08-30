import 'dart:convert';

class MemberStore {
  Object load(String body) {
    final m = jsonDecode(body); // want: dart-jsondecode-requires-fromjson-narrowing
    return m;
  }
}
