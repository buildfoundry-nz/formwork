import 'package:flutter/widgets.dart';

class PdfUnderlay extends StatefulWidget {
  const PdfUnderlay({required this.projectId, required this.pageNumber});
  final String projectId;
  final int pageNumber;
  @override
  State<PdfUnderlay> createState() => _PdfUnderlayState();
}

(String, int) _fingerprintOf(PdfUnderlay w) => (w.projectId, w.pageNumber);

class _PdfUnderlayState extends State<PdfUnderlay> {
  @override
  Widget build(BuildContext context) => const SizedBox();
}
