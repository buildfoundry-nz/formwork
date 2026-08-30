import 'package:flutter/widgets.dart';

class _BlankStateView extends StatelessWidget { // want: no-locally-defined-empty-state-widgets
  const _BlankStateView();

  @override
  Widget build(BuildContext context) => const SizedBox.shrink();
}
