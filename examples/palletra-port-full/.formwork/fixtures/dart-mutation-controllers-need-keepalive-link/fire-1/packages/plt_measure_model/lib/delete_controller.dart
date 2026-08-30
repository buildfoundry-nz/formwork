import 'package:riverpod_annotation/riverpod_annotation.dart';

part 'delete_controller.g.dart';

@riverpod
class RemoveSectionAnnotationsController
    extends _$RemoveSectionAnnotationsController {
  @override
  bool build(String segmentId) => false;

  Future<int> deleteAll() async {
    state = true;
    final affected = await _repo.purgeSection(segmentId);
    state = false;
    return affected.count;
  }
}
