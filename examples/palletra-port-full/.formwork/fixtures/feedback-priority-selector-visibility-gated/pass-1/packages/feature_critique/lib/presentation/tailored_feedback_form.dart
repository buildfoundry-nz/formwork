import 'package:flutter/material.dart';

class TailoredFeedbackForm extends ConsumerWidget {
  const TailoredFeedbackForm({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final displayPriority = ref.watch(feedbackPriorityVisibilityProvider);
    if (!displayPriority) return const SizedBox.shrink();
    return const UrgencySegmentedButton();
  }
}
