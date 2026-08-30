// impactedPageJson() carries an inline copy instead of reading from disk.
Future<String> impactedPageJson() async {
  return '{"pageId":"p1","annotations":[],"metrics":{}}';
}

// COMMENT-IMMUNITY PROOF (code-only-dart). The line below is the real
// satisfying text from pass-1, in a comment. This fixture must STILL
// fire: delete `preprocess: code-only-dart` from the rule and it stops, which
// is the regression this pins (#263.3 class).
// return File(path).readAsString();
