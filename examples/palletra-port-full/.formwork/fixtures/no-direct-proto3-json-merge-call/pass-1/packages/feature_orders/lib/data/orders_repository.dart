import 'package:plt_core/network/wire_dio.dart';

import 'orders.pb.dart';

class RequisitionsRepository {
  RequisitionsRepository(this._dio);

  final WireDio _dio;

  Future<OrdersResponse> fetch(String id) {
    // Rides the canonical decoder; no open-coded mergeFromProto3Json here.
    return _dio.get('/api/orders/$id', OrdersResponse.new);
  }
}
