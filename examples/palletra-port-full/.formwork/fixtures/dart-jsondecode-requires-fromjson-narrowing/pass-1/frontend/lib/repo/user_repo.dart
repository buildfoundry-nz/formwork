import 'dart:convert';

import 'user.dart';

class MemberStore {
  User load(String body) {
    return User.fromJson(jsonDecode(body) as Map<String, dynamic>);
  }
}
