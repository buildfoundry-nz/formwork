import 'package:flutter/widgets.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

class ReaderScreen extends ConsumerWidget {
  const ReaderScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final displayAnnotations =
        ref.watch(layerToggleProvider.select((s) => s.displayAnnotations));
    return Text('$displayAnnotations');
  }
}
