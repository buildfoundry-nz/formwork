import 'package:riverpod_annotation/riverpod_annotation.dart';

part 'providers.g.dart';

@riverpod
List<int> liveInputs(LiveInputsRef ref) => const [1, 2, 3];

// This annotated source function still exists but its generated
// `deadInputsProvider` is referenced by no live consumer — dead symbol.
@riverpod
List<int> deadInputs(DeadInputsRef ref) => const [4, 5, 6];
