import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

class PlotSidebarFooter extends ConsumerWidget {
  const PlotSidebarFooter({super.key});

  Future<void> _onEndorse(WidgetRef ref) async {
    // Side-effects run on the keepAlive controller's own ref, not this one.
    await ref.read(confirmControllerProvider.notifier).approveAndProceed();
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return TextButton(
      onPressed: () => _onEndorse(ref),
      child: const Text('Approve'),
    );
  }
}
