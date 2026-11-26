import 'package:flutter/widgets.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'providers.dart';

class LinesScreen extends ConsumerWidget {
  const LinesScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final live = ref.watch(liveInputsProvider);
    final dead = ref.watch(deadInputsProvider);
    return Text('${live.length + dead.length}');
  }
}
