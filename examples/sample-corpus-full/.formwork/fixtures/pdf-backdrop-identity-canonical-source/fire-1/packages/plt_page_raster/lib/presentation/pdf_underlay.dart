import 'package:flutter/widgets.dart';

class PdfUnderlay extends StatefulWidget {
  const PdfUnderlay({required this.projectId, required this.pageNumber});
  final String projectId;
  final int pageNumber;
  @override
  State<PdfUnderlay> createState() => _PdfUnderlayState();
}

class _PdfUnderlayState extends State<PdfUnderlay> {
  // The single-source _fingerprintOf function has been inlined away — the anchor
  // is gone, so RULE A must fire (a rename/removal must not pass vacuously).
  @override
  Widget build(BuildContext context) => const SizedBox();
}
