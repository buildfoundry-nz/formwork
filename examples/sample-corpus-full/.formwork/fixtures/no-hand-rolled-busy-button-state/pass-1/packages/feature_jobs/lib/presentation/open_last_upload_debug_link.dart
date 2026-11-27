import 'package:flutter/material.dart';
import 'package:plt_widgets/busy_filled_button.dart';

class OpenLastUploadDiagnosticLink extends StatefulWidget {
  const OpenLastUploadDiagnosticLink({super.key});
  @override
  State<OpenLastUploadDiagnosticLink> createState() => _S();
}

class _S extends State<OpenLastUploadDiagnosticLink> {
  // A plain re-entry latch guarding a modal launch: FP1 field present, but no
  // setState toggle, no `_pending ?` ternary, and no busy:/isOccupied: pass-to-child.
  bool _pending = false;

  Future<void> _open() async {
    if (_pending) return;
    _pending = true;
    await showDialog<void>(context: context, builder: (_) => const SizedBox());
    _pending = false;
  }

  @override
  Widget build(BuildContext context) {
    // The real spinner affordance rides BusyOutlinedButton.
    return BusyOutlinedButton(onPressed: _open, child: const Text('Open'));
  }
}
