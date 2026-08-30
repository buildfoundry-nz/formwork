import 'package:riverpod_annotation/riverpod_annotation.dart';
import 'package:feature_dimension/data/page_raster_repository.dart';

part 'page_raster_store.g.dart';

@Riverpod(keepAlive: true) // want: dart-keepalive-caches-must-register-invalidation
class PagePaintStore extends _$PagePaintStore {
  @override
  Map<int, PagePaint> build() => const {};

  Future<void> ensure(int page) async {
    final repo = ref.read(pagePaintRepositoryProvider);
    final render = await repo.fetch(page);
    state = {...state, page: render};
  }
}
