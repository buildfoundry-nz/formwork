import 'skus_repository.dart';

// Replaces the fake stub with the live proto-backed reader.
class ProtoSkusRepository implements SkusRepository {
  ProtoSkusRepository(this._client);
  final SkusClient _client;

  @override
  Future<List<Sku>> list() => _client.fetchSkus();
}
