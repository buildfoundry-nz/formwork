import 'package:riverpod_annotation/riverpod_annotation.dart';

part 'providers.g.dart';

@riverpod
List<int> liveInputs(LiveInputsRef ref) => const [1, 2, 3];

// This annotated source function still exists but the generated provider it
// produces is referenced by no live consumer — a dead symbol.
@riverpod
List<int> deadInputs(DeadInputsRef ref) => const [4, 5, 6];
