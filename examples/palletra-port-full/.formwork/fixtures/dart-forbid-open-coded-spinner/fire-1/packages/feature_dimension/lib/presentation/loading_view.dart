import 'package:flutter/material.dart';

class BusyView extends StatelessWidget {
  const BusyView({super.key});

  @override
  Widget build(BuildContext context) {
    return const Center(child: CircularProgressIndicator()); // want: dart-forbid-open-coded-spinner
  }
}
