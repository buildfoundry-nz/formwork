import 'dart:convert';

import 'orders.pb.dart';

class RequisitionsRepository {
  OrdersResponse decode(String body) {
    final decoded = OrdersResponse();
    return decoded..mergeFromProto3Json(jsonDecode(body)); // want: no-direct-proto3-json-merge-call
  }
}
