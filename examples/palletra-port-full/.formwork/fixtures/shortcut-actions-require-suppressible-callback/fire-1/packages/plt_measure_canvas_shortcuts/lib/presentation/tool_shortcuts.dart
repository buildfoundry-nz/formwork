import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

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
          DualPaneIntent: CallbackAction<DualPaneIntent>( // want: shortcut-actions-require-suppressible-callback
            onInvoke: (intent) => _toggleDualPane(),
          ),
        },
        child: child,
      ),
    );
  }
}
