// Thin app-shell source (post-#7477 monorepo). Clean sample so the Flutter
// lib scope is non-empty for the ported rules.
import 'package:flutter/material.dart';

class PalletraApp extends StatelessWidget {
  const PalletraApp({super.key});

  @override
  Widget build(BuildContext context) {
    return const MaterialApp(home: Scaffold());
  }
}
