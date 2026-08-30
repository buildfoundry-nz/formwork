import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:plt_measure_hotkeys/presentation/guarded_shortcut_action.dart';

class ToolHotkeys extends StatelessWidget {
  const ToolHotkeys({super.key, required this.child});
  final Widget child;

  @override
  Widget build(BuildContext context) {
    return Shortcuts(
      shortcuts: <ShortcutActivator, Intent>{
        const SingleActivator(LogicalKeyboardKey.digit2): const DualPaneIntent(),
      },
      child: Actions(
        actions: <Type, Action<Intent>>{
          DualPaneIntent: GuardedCallbackAction<DualPaneIntent>(
            suppressed: () => _aFieldHasFocus(),
            onInvoke: (intent) => _toggleDualPane(),
          ),
        },
        child: child,
      ),
    );
  }
}
