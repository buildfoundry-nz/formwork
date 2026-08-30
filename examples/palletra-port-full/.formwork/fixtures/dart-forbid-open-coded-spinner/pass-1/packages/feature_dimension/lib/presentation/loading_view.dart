import 'package:flutter/material.dart';
import 'package:plt_widgets/plt_widgets.dart';

class BusyView extends StatelessWidget {
  const BusyView({super.key});

  @override
  Widget build(BuildContext context) {
    return const Center(child: RemoteLoadingView());
  }
}
