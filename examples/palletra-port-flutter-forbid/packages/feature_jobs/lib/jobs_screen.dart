// Jobs screen renders dock-status labels from the server verbatim.
import 'package:flutter/material.dart';

class TasksScreen extends StatelessWidget {
  const TasksScreen({super.key, required this.options});

  final List<DockStatusOption> options;

  @override
  Widget build(BuildContext context) {
    return Column(
      children: [
        for (final o in options) Text(dockStatusOptionLabel(o)),
      ],
    );
  }
}

String dockStatusOptionLabel(DockStatusOption o) => o.label;

class DockStatusOption {
  const DockStatusOption({required this.value, required this.label});
  final String value;
  final String label;
}
