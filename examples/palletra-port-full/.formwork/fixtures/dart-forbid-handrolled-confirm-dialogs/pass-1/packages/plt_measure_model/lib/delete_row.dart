import 'package:flutter/material.dart';
import 'package:plt_widgets/verify_action_dialog.dart';

Future<bool?> confirmDelete(BuildContext context) {
  return showDecisionDialog<bool>(
    context: context,
    title: 'Delete section?',
    message: 'This removes every line in the section.',
    confirmLabel: 'Delete',
    destructive: true,
  );
}
