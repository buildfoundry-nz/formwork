import 'package:feature_costing/data/products_repo.dart';
class StockroomProductsRepo {
  Future<Products> list() => WireDio.get(productsEndpoint);
}
