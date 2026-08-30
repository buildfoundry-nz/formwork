import 'package:flutter/widgets.dart';

class ExtentLabel extends StatelessWidget {
  const ExtentLabel(this.area, {super.key});

  final double area;

  @override
  Widget build(BuildContext context) {
    return Text('${area.toStringAsFixed(2)} m2'); // want: no-raw-tostringasfixed-in-presentation
  }
}
