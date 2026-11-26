import 'package:flutter/material.dart';
import 'package:plt_core/network/customer_facing_error.dart';

Widget buildError(BuildContext context, Object err) {
  return Text(explainError(err));
}
