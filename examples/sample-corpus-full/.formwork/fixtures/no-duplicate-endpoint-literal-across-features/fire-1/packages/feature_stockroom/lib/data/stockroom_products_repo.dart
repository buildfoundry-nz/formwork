class StockroomProductsRepo {
  Future<Products> list() => WireDio.get('/api/catalog/products?search=roof');
}
