import 'package:flutter/material.dart';

Future<void> goHome(BuildContext context, bool mounted) async {
  await Future<void>.delayed(Duration.zero);
  if (!mounted) return;
  Navigator.of(context).pushNamed('/home');
}
