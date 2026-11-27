import 'package:flutter/material.dart';

class TailoredFeedbackForm extends ConsumerWidget {
  const TailoredFeedbackForm({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    // TODO: gate this on feedbackPriorityVisibilityProvider (staff only)
    return const UrgencySegmentedButton();
  }
}
