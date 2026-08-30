import 'package:flutter/material.dart';
import 'package:plt_core/plt_core.dart';
import 'package:plt_widgets/plt_widgets.dart';

class PlotError {
  // The sanctioned helper: showAlertSnackBar( ends with SnackBar( but is
  // preceded by an identifier char, so the non-identifier guard never anchors.
  static void show(ScaffoldMessengerState messenger, Object err) {
    showAlertSnackBar(messenger, err);
  }

  // An inline error view is a legitimate explainError consumer (not a snackbar).
  static String label(Object err) {
    final message = explainError(err);
    return message;
  }
}
