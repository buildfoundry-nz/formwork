import 'skus_repository.dart';

class StubSkusRepository implements SkusRepository { // want: skus-no-fixtures-in-lib
  @override
  Future<List<Sku>> list() async => const <Sku>[];
}
