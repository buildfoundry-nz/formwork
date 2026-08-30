import 'package:plt_widgets/shell_card.dart';

Widget composePanel(BuildContext context) {
  // ShellCard( has an identifier char before Card, so it never matches.
  return ShellCard(child: const Text('hello'));
}
