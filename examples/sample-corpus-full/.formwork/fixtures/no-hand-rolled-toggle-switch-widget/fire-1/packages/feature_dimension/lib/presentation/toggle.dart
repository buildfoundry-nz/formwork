import 'package:flutter/material.dart';

class Toggles {
  Widget a(bool v, ValueChanged<bool> f) =>
      Switch(value: v, onChanged: f); // want: no-hand-rolled-toggle-switch-widget
  Widget b(bool v, ValueChanged<bool> f) =>
      SwitchListTile(value: v, onChanged: f); // want: no-hand-rolled-toggle-switch-widget
  Widget c(bool v, ValueChanged<bool> f) =>
      Switch.adaptive(value: v, onChanged: f); // want: no-hand-rolled-toggle-switch-widget
}
