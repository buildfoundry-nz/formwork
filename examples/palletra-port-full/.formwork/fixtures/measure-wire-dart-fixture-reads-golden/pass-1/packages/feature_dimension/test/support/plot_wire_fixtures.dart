import 'dart:io';

// impactedPageJson() reads the golden from disk.
Future<String> impactedPageJson() {
  final path = 'testdata/plot_wire_affected_page.golden.json';
  return File(path).readAsString();
}
