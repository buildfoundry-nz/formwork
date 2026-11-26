import 'package:flutter/material.dart';

Widget buildError(BuildContext context, Object err) {
  return Text('Upload failed: $err'); // want: no-bare-error-in-ui-text
}
