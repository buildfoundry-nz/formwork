import 'dart:io';

// The Dart mirror names the single committed golden and reads it from disk.
Future<String> impactedPageJson() {
  final path = 'testdata/plot_wire_affected_page.golden.json';
  return File(path).readAsString();
}
