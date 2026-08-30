import 'package:flutter/material.dart';
import 'package:plt_core/plt_core.dart';

class PlotError {
  static void show(ScaffoldMessengerState messenger, Object err) {
    messenger.showSnackBar( // want: no-custom-error-snackbar-implementations
      SnackBar(
        content: Text(explainError(err)),
      ),
    );
  }
}
