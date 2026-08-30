import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

class PlotSidebarFooter extends ConsumerWidget {
  const PlotSidebarFooter({super.key});

  Future<void> _onEndorse(WidgetRef ref) async {
    await ref.read(confirmControllerProvider.notifier).approve();
    ref.invalidate(plotSectionsProvider); // want: footer-handler-no-ref-post-await
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return TextButton(
      onPressed: () => _onEndorse(ref),
      child: const Text('Approve'),
    );
  }
}
