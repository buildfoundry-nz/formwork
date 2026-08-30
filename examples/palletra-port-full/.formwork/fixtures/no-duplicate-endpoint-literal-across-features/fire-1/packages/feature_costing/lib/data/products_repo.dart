class ProductsRepo {
  Future<Products> list() => WireDio.get('/api/catalog/products?q=roof');
}
