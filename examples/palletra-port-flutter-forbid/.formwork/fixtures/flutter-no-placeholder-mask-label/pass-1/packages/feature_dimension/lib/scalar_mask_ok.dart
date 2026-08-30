import 'package:flutter/material.dart';

// Clean: render the server-formatted scalar value verbatim.
Widget scalarMask(step) {
  return Text(step.scalarValue);
}
