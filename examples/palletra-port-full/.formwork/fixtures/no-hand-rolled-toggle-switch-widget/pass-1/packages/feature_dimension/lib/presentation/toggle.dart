import 'package:plt_widgets/plt_widgets.dart';

class Toggles {
  Widget a(bool v, ValueChanged<bool> f) => AppSwitch(value: v, onChanged: f);
  Widget b(bool v, ValueChanged<bool> f) => ShellSwitchTile(value: v, onChanged: f);

  void tap(Repo repo, Mode mode) {
    repo.setToggle(true);
    final primaryToggle = compute();
    switch (mode) {
      case Mode.on:
        break;
      case Mode.off:
        break;
    }
    return primaryToggle;
  }
}
