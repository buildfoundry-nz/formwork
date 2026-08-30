import 'package:flutter/widgets.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

class ReaderScreen extends ConsumerWidget {
  const ReaderScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final layers = ref.watch(layerToggleProvider); // want: dart-multi-field-providers-require-select
    return Text('${layers.displayAnnotations}');
  }
}
