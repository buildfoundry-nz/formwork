import 'package:flutter/material.dart';

Future<void> goHome(BuildContext context) async {
  await Future<void>.delayed(Duration.zero);
  // ignore: use_build_context_synchronously  -- want: dart-forbid-build-context-lint-suppression
  Navigator.of(context).pushNamed('/home');
}
