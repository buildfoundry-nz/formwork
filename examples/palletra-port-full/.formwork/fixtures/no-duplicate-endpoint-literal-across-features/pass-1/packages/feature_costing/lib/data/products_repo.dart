const productsEndpoint = '/api/catalog/products';
class ProductsRepo {
  Future<Products> list() => WireDio.get(productsEndpoint);
}
