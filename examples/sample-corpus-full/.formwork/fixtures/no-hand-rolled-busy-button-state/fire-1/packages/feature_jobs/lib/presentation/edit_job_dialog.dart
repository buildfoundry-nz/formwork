// want: no-hand-rolled-busy-button-state
import 'package:flutter/material.dart';

class ModifyJobDialog extends StatefulWidget {
  const ModifyJobDialog({super.key});
  @override
  State<ModifyJobDialog> createState() => _ModifyJobDialogState();
}

class _ModifyJobDialogState extends State<ModifyJobDialog> {
  bool _submitting = false;

  Future<void> _save() async {
    setState(() => _submitting = true);
    await Future<void>.delayed(const Duration(seconds: 1));
    setState(() => _submitting = false);
  }

  @override
  Widget build(BuildContext context) {
    return FilledButton(
      onPressed: _submitting ? null : _save,
      child: _submitting
          ? const CircularProgressIndicator()
          : const Text('Save'),
    );
  }
}
