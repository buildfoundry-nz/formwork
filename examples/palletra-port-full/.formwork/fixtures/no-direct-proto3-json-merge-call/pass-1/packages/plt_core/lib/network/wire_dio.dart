import 'dart:convert';

// The canonical decoder file is the sanctioned owner of the open-coded
// mergeFromProto3Json(jsonDecode(...)) shape; except.paths carves it out.
class WireDio {
  T decode<T extends GeneratedMessage>(String body, T Function() create) {
    if (body.isEmpty) return create();
    return create()..mergeFromProto3Json(jsonDecode(body));
  }
}
