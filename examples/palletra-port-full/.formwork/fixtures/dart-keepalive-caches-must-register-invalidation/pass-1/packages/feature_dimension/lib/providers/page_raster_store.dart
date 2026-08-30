import 'package:riverpod_annotation/riverpod_annotation.dart';
import 'package:feature_dimension/data/page_raster_repository.dart';
import 'package:plt_core/session_store.dart';

part 'page_raster_store.g.dart';

@Riverpod(keepAlive: true)
class PagePaintStore extends _$PagePaintStore {
  @override
  Map<int, PagePaint> build() {
    pinInProjectCache(ref);
    return const {};
  }

  Future<void> ensure(int page) async {
    final repo = ref.read(pagePaintRepositoryProvider);
    final render = await repo.fetch(page);
    state = {...state, page: render};
  }
}
